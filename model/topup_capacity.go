package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 钱包容量：充值订单在「下单时」就确认入账后不会超过钱包上限。
//
// 入账环节（RechargeEpay、creditTronOrderTx）一直拦着「充值后额度超出上限」，但那时客户
// 已经付了钱：超限的订单只能留在 pending，等对账巡检告警后人工处理。上限是 common.MaxQuota
// （额度按 32 位整数对待），QuotaPerUnit 为 500000 时约合 4294.97 个充值单位——单笔
// 2000 的充值连充三次、或者余额两千多再充 2000，就会撞上。
//
// 这里把同一道算术提前到下单：
//
//	余额 + 仍可能被付款的待付订单 + 本次 ≤ 上限
//
// 判断与建单在同一个事务里，并先锁住用户行：同一用户的并发下单因此串行化，后到的看得见
// 先到的那张待付单。入账环节的检查保留，兜住这里兜不住的情形（过了预留窗口才付款、
// 兑换码或管理员加额度挤占了容量）。

// TopUpReservationWindowSeconds 是非 TRON 渠道的待付订单占着钱包容量的时长。
//
// 取舍：太短，客户在窗口外付款仍会卡在入账环节；太长，放弃支付的订单会挡住客户自己
// 重新下单。依据是 2026-10-11 核对的生产数据——有完成时间的易支付订单从下单到入账
// 最长 123 秒。TRON 订单不用这个窗口，按它自己的有效期（ExpiresAtMS）算。
const TopUpReservationWindowSeconds int64 = 15 * 60

// ErrTopUpWalletFull：这笔充值入账后会超过钱包上限，订单没有创建。
var ErrTopUpWalletFull = errors.New("topup exceeds wallet capacity")

// TopUpCapacity 是一次容量判断的账目，单位都是额度。
type TopUpCapacity struct {
	Balance  int64 // 当前余额（欠费时为负）
	Reserved int64 // 仍可能被付款的待付订单合计
	Pending  int   // 这些待付订单的笔数
	Limit    int64 // 钱包上限
}

// Fits 报告再充 creditQuota 之后是否仍在上限以内。单笔本身不得超过上限：欠费余额
// 不放宽这一条（入账环节同样拒绝超过上限的单笔额度）。
func (c TopUpCapacity) Fits(creditQuota int64) bool {
	if creditQuota <= 0 || creditQuota > c.Limit {
		return false
	}
	return c.Balance+c.Reserved+creditQuota <= c.Limit
}

// Room 是现在最多还能充多少额度：不为负，也不超过单笔上限。
func (c TopUpCapacity) Room() int64 {
	room := c.Limit - c.Balance - c.Reserved
	if room < 0 {
		return 0
	}
	if room > c.Limit {
		return c.Limit
	}
	return room
}

// TopUpWalletFullError 带着容量账目，供调用方给客户一句说得清的提示。
// errors.Is(err, ErrTopUpWalletFull) 为真。
type TopUpWalletFullError struct {
	Capacity    TopUpCapacity
	CreditQuota int64
}

func (e *TopUpWalletFullError) Error() string {
	return fmt.Sprintf("%s: balance=%d reserved=%d pending=%d credit=%d limit=%d",
		ErrTopUpWalletFull.Error(), e.Capacity.Balance, e.Capacity.Reserved, e.Capacity.Pending,
		e.CreditQuota, e.Capacity.Limit)
}

func (e *TopUpWalletFullError) Is(target error) bool {
	return target == ErrTopUpWalletFull
}

// topUpCreditQuota 是这张订单入账时会加的额度，与各渠道结算函数同一口径：
// Stripe 按 Money × QuotaPerUnit；Creem 的 Amount 本身就是额度；其余按 Amount × QuotaPerUnit。
//
// 结果被夹在 [0, 上限+1]：先比较完整的十进制数再转整数。数量离谱的订单乘出来会超过
// int64，直接转换会绕回成一个看似正常的值（2^64 附近的乘积绕回后是个小正数）——那样的
// 订单要么通过容量判断，要么把占用算小。超过上限的一律算「上限 + 1」，非正数算 0。
func topUpCreditQuota(topUp *TopUp) int64 {
	overLimit := int64(common.MaxQuota) + 1
	var credit decimal.Decimal
	switch topUp.PaymentProvider {
	case PaymentProviderStripe:
		// decimal.NewFromFloat 遇到 NaN / Inf 会 panic
		if math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 1) {
			return overLimit
		}
		if math.IsInf(topUp.Money, -1) {
			return 0
		}
		credit = decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	case PaymentProviderCreem:
		credit = decimal.NewFromInt(topUp.Amount)
	default:
		credit = decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	if credit.Sign() <= 0 {
		return 0
	}
	if credit.GreaterThan(decimal.NewFromInt(int64(common.MaxQuota))) {
		return overLimit
	}
	return credit.IntPart()
}

// topUpCapacityTx 锁住用户行并算出当前的容量账目。必须在事务里调用，调用方随后在同一
// 事务里建单，「判断」与「占用」之间才没有空隙。
func topUpCapacityTx(tx *gorm.DB, userID int, nowMS int64) (TopUpCapacity, error) {
	user := User{}
	if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", userID).First(&user).Error; err != nil {
		return TopUpCapacity{}, err
	}
	capacity := TopUpCapacity{Balance: int64(user.Quota), Limit: int64(common.MaxQuota)}

	var recent []TopUp
	since := nowMS/1000 - TopUpReservationWindowSeconds
	if err := tx.Where("user_id = ? AND status = ? AND payment_provider <> ? AND create_time > ?",
		userID, common.TopUpStatusPending, PaymentProviderTron, since).Find(&recent).Error; err != nil {
		return TopUpCapacity{}, err
	}
	for i := range recent {
		capacity.Reserved += topUpCreditQuota(&recent[i])
	}
	capacity.Pending = len(recent)

	var tronQuotas []int64
	if err := tx.Table("tron_topup_orders").
		Joins("JOIN top_ups ON top_ups.id = tron_topup_orders.top_up_id").
		Where("tron_topup_orders.user_id = ? AND tron_topup_orders.expires_at_ms > ? AND top_ups.status = ?",
			userID, nowMS, common.TopUpStatusPending).
		Pluck("tron_topup_orders.credit_quota", &tronQuotas).Error; err != nil {
		return TopUpCapacity{}, err
	}
	for _, quota := range tronQuotas {
		capacity.Reserved += quota
	}
	capacity.Pending += len(tronQuotas)
	return capacity, nil
}

// requireTopUpRoomTx 在事务里确认还装得下 creditQuota，装不下返回 *TopUpWalletFullError。
func requireTopUpRoomTx(tx *gorm.DB, userID int, creditQuota int64, nowMS int64) error {
	capacity, err := topUpCapacityTx(tx, userID, nowMS)
	if err != nil {
		return err
	}
	if !capacity.Fits(creditQuota) {
		return &TopUpWalletFullError{Capacity: capacity, CreditQuota: creditQuota}
	}
	return nil
}

// InsertTopUpWithinWalletCapacity 在同一事务里确认钱包容量并插入订单（易支付等非 TRON 渠道）。
// 装不下时返回 *TopUpWalletFullError，订单不创建。
func InsertTopUpWithinWalletCapacity(topUp *TopUp) error {
	if topUp == nil || topUp.UserId <= 0 {
		return errors.New("invalid topup order")
	}
	originalID := topUp.Id
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := requireTopUpRoomTx(tx, topUp.UserId, topUpCreditQuota(topUp), topUp.CreateTime*1000); err != nil {
			return err
		}
		return tx.Create(topUp).Error
	})
	if err != nil {
		topUp.Id = originalID
	}
	return err
}
