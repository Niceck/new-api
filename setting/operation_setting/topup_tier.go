package operation_setting

import (
	"encoding/json" // 只引用 json.Number 这个类型；解析走 common.UnmarshalJsonStr
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// 累计充值等级（2026-10-30 站长定）：累计到账面值达到门槛后，以后每一笔充值（任意金额）都按该档折扣付款；
// 与单笔档位折扣（AmountDiscount）取对客户更优的一个，不叠加；累计只增不减，所以等级只升不降。
// 门槛与折扣是配置项 payment_setting.tier_discount，例如 {"1000":0.98,"2500":0.97,"5000":0.95,"10000":0.92}。
// 写 {} 即关闭等级（下单时不再读累计）。

// TierDiscountOptionKey 配置键；写入前必须过 ValidateTierDiscountJSON（模型层与管理接口都挂了）。
const TierDiscountOptionKey = "payment_setting.tier_discount"

const (
	// 等级最多几档：后台误填一长串也不至于把钱包页撑坏。
	maxTopUpTiers = 20
	// 累计面值门槛上限：再大没有业务意义，也别让 remaining 之类的算术靠近 int64 边界。
	maxTopUpTierThreshold = 1_000_000_000
	// 折扣最多两位小数：等级对任意整数金额生效，只有两位小数乘整数才恒为整分；
	// 易支付入账前严格比对回调金额，应付金额不能带半分钱。
	tierDiscountDecimals = 2
)

// TopUpTier 一档累计充值等级。
type TopUpTier struct {
	Level     int     `json:"level"`     // 从 1 起，按门槛升序
	Threshold int64   `json:"threshold"` // 累计到账面值门槛（下单口径的整数单位）
	Discount  float64 `json:"discount"`  // 实付比例，0.92 = 9.2 折
}

// ErrTierDiscountInvalid 配置不合法时的统一前缀，便于后台与运维脚本按前缀识别。
var ErrTierDiscountInvalid = errors.New("累计充值等级配置不合法")

// tierKeyPattern 数一数原文里有几个键：json 解析遇到重复键会悄悄取最后一个。
var tierKeyPattern = regexp.MustCompile(`"([^"\\]*)"\s*:`)

// tiersFromMap 把进程内的配置表整理成按门槛升序、带等级号的等级表。任何一项不合法就整体报错，
// 不悄悄丢掉。写入时已经过严格校验，这里是对运行态的兜底复核。
func tiersFromMap(raw map[int]float64) ([]TopUpTier, error) {
	if len(raw) > maxTopUpTiers {
		return nil, fmt.Errorf("%w：最多 %d 档，现在有 %d 档", ErrTierDiscountInvalid, maxTopUpTiers, len(raw))
	}
	tiers := make([]TopUpTier, 0, len(raw))
	for threshold, discount := range raw {
		if threshold <= 0 || threshold > maxTopUpTierThreshold {
			return nil, fmt.Errorf("%w：门槛必须是 1 到 %d 之间的整数，收到 %d", ErrTierDiscountInvalid, maxTopUpTierThreshold, threshold)
		}
		if !(discount > 0 && discount <= 1) {
			return nil, fmt.Errorf("%w：门槛 %d 的折扣 %v 必须大于 0 且不超过 1（0.92 表示 9.2 折）", ErrTierDiscountInvalid, threshold, discount)
		}
		if !hasAtMostTwoDecimals(discount) {
			return nil, fmt.Errorf("%w：门槛 %d 的折扣 %v 最多两位小数", ErrTierDiscountInvalid, threshold, discount)
		}
		tiers = append(tiers, TopUpTier{Threshold: int64(threshold), Discount: discount})
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Threshold < tiers[j].Threshold })
	for i := range tiers {
		tiers[i].Level = i + 1
		if i > 0 && tiers[i].Discount > tiers[i-1].Discount {
			return nil, fmt.Errorf("%w：门槛 %d 的折扣 %v 比门槛 %d 的 %v 更差，越高档应越优惠",
				ErrTierDiscountInvalid, tiers[i].Threshold, tiers[i].Discount, tiers[i-1].Threshold, tiers[i-1].Discount)
		}
	}
	return tiers, nil
}

// hasAtMostTwoDecimals 运行态的浮点复核；写入时用十进制严格校验（见 ValidateTierDiscountJSON）。
func hasAtMostTwoDecimals(discount float64) bool {
	scaled := discount * 100
	return math.Abs(scaled-math.Round(scaled)) < 1e-9
}

// ValidateTierDiscountJSON 写入 payment_setting.tier_discount 前的严格校验（模型层与管理接口共用）：
// 必须是 {"正整数门槛": 折扣} 的 JSON 对象，门槛不带前导零 / 符号 / 指数且不重复、不超过上限，
// 折扣按十进制解析、在 (0, 1] 内且最多两位小数，越高档越优惠，最多 20 档；
// 按 tokens 展示的站点不允许开启（各支付渠道订单面值的单位不一致，累计会算错）。
func ValidateTierDiscountJSON(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%w：不能为空，要关闭等级请写 {}", ErrTierDiscountInvalid)
	}
	var raw map[string]json.Number
	if err := common.UnmarshalJsonStr(trimmed, &raw); err != nil {
		return fmt.Errorf("%w：应为 {\"累计门槛\": 折扣} 的 JSON 对象（%v）", ErrTierDiscountInvalid, err)
	}
	if keys := tierKeyPattern.FindAllStringIndex(trimmed, -1); len(keys) != len(raw) {
		return fmt.Errorf("%w：有重复的门槛键", ErrTierDiscountInvalid)
	}
	if len(raw) > maxTopUpTiers {
		return fmt.Errorf("%w：最多 %d 档，现在有 %d 档", ErrTierDiscountInvalid, maxTopUpTiers, len(raw))
	}
	one := decimal.NewFromInt(1)
	tiers := make(map[int]float64, len(raw))
	for key, number := range raw {
		threshold, err := strconv.ParseInt(key, 10, 64)
		if err != nil || strconv.FormatInt(threshold, 10) != key {
			return fmt.Errorf("%w：门槛 %q 必须是不带前导零、符号与指数的正整数", ErrTierDiscountInvalid, key)
		}
		if threshold <= 0 || threshold > maxTopUpTierThreshold {
			return fmt.Errorf("%w：门槛必须是 1 到 %d 之间的整数，收到 %d", ErrTierDiscountInvalid, maxTopUpTierThreshold, threshold)
		}
		if _, dup := tiers[int(threshold)]; dup {
			return fmt.Errorf("%w：门槛 %d 重复", ErrTierDiscountInvalid, threshold)
		}
		rate, err := decimal.NewFromString(number.String())
		if err != nil || !rate.IsPositive() || rate.GreaterThan(one) {
			return fmt.Errorf("%w：门槛 %d 的折扣 %s 必须大于 0 且不超过 1（0.92 表示 9.2 折）", ErrTierDiscountInvalid, threshold, number.String())
		}
		if !rate.Equal(rate.Round(tierDiscountDecimals)) {
			return fmt.Errorf("%w：门槛 %d 的折扣 %s 最多两位小数", ErrTierDiscountInvalid, threshold, number.String())
		}
		tiers[int(threshold)] = rate.InexactFloat64()
	}
	if _, err := tiersFromMap(tiers); err != nil {
		return err
	}
	if len(tiers) > 0 && GetQuotaDisplayType() == QuotaDisplayTypeTokens {
		return fmt.Errorf("%w：按 tokens 展示时各支付渠道订单面值的单位不一致，累计充值等级不支持；请先改回按货币展示", ErrTierDiscountInvalid)
	}
	return nil
}

var (
	badTierConfigMu   sync.Mutex
	badTierConfigSeen string
)

// noteBadTierConfig 运行态发现配置不合法（本不该写得进来，只可能是直接改库）：按没有等级处理，
// 但要记一条错误日志，别让数据库故障级别的事静默成「没配置等级」。同一份坏配置只记一次。
func noteBadTierConfig(raw map[int]float64, err error) {
	fingerprint := fmt.Sprintf("%v", raw) // map 的打印是按键排序的，可当指纹
	badTierConfigMu.Lock()
	defer badTierConfigMu.Unlock()
	if badTierConfigSeen == fingerprint {
		return
	}
	badTierConfigSeen = fingerprint
	common.SysError("累计充值等级配置不合法，已按没有等级处理（客户按单笔档位价付款）：" + err.Error())
}

// TopUpTiers 当前生效的等级表（按门槛升序）。配置为空返回 nil，即没有等级；
// 配置不合法也返回 nil（客户按原价 / 单笔档位价付款，不会多扣），并记错误日志。
func TopUpTiers() []TopUpTier {
	raw := GetPaymentSetting().TierDiscount
	tiers, err := tiersFromMap(raw)
	if err != nil {
		noteBadTierConfig(raw, err)
		return nil
	}
	if len(tiers) == 0 {
		return nil
	}
	return tiers
}

// TierForTotal 按累计到账面值找当前等级与下一档。没达到任何门槛时 current 为 nil；
// 已是最高档时 next 为 nil。
func TierForTotal(tiers []TopUpTier, total int64) (current *TopUpTier, next *TopUpTier) {
	for i := range tiers {
		if total >= tiers[i].Threshold {
			current = &tiers[i]
			continue
		}
		next = &tiers[i]
		break
	}
	return current, next
}
