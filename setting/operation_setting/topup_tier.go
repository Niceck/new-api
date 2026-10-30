package operation_setting

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// 累计充值等级（2026-10-30 站长定）：累计到账面值达到门槛后，以后每一笔充值（任意金额）都按该档折扣付款；
// 与单笔档位折扣（AmountDiscount）取对客户更优的一个，不叠加；累计只增不减，所以等级只升不降。
// 门槛与折扣是配置项 payment_setting.tier_discount，例如 {"1000":0.98,"2500":0.97,"5000":0.95,"10000":0.92}。
// 写 {} 即关闭等级（下单时不再读累计）。

// 等级最多几档：后台误填一长串也不至于把钱包页撑坏。
const maxTopUpTiers = 20

// TopUpTier 一档累计充值等级。
type TopUpTier struct {
	Level     int     `json:"level"`     // 从 1 起，按门槛升序
	Threshold int64   `json:"threshold"` // 累计到账面值门槛（下单口径的整数单位）
	Discount  float64 `json:"discount"`  // 实付比例，0.92 = 9.2 折
}

// ErrTierDiscountInvalid 配置不合法时的统一前缀，便于后台与运维脚本按前缀识别。
var ErrTierDiscountInvalid = errors.New("累计充值等级配置不合法")

// tiersFromMap 把配置表整理成按门槛升序、带等级号的等级表。任何一项不合法就整体报错，
// 不悄悄丢掉：配置框架对解析失败是静默跳过的，所以写入前的校验是唯一的闸门。
func tiersFromMap(raw map[int]float64) ([]TopUpTier, error) {
	if len(raw) > maxTopUpTiers {
		return nil, fmt.Errorf("%w：最多 %d 档，现在有 %d 档", ErrTierDiscountInvalid, maxTopUpTiers, len(raw))
	}
	tiers := make([]TopUpTier, 0, len(raw))
	for threshold, discount := range raw {
		if threshold <= 0 {
			return nil, fmt.Errorf("%w：门槛必须是正整数，收到 %d", ErrTierDiscountInvalid, threshold)
		}
		if !(discount > 0 && discount <= 1) {
			return nil, fmt.Errorf("%w：门槛 %d 的折扣 %v 必须大于 0 且不超过 1（0.92 表示 9.2 折）", ErrTierDiscountInvalid, threshold, discount)
		}
		if !hasAtMostTwoDecimals(discount) {
			// 任意整数金额乘两位小数才恒为整分；易支付入账前严格比对回调金额，应付金额不能带半分钱。
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

func hasAtMostTwoDecimals(discount float64) bool {
	scaled := discount * 100
	return math.Abs(scaled-math.Round(scaled)) < 1e-6
}

// ValidateTierDiscountJSON 供后台 / 运维脚本写入 payment_setting.tier_discount 时校验。
func ValidateTierDiscountJSON(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w：不能为空，要关闭等级请写 {}", ErrTierDiscountInvalid)
	}
	var raw map[int]float64
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return fmt.Errorf("%w：应为 {\"累计门槛\": 折扣} 的 JSON 对象（%v）", ErrTierDiscountInvalid, err)
	}
	_, err := tiersFromMap(raw)
	return err
}

// TopUpTiers 当前生效的等级表（按门槛升序）。配置为空或不合法时返回 nil，即没有等级；
// 不合法的配置本不该写得进来（见 ValidateTierDiscountJSON），这里只是兜底不让下单路径崩掉。
func TopUpTiers() []TopUpTier {
	tiers, err := tiersFromMap(GetPaymentSetting().TierDiscount)
	if err != nil || len(tiers) == 0 {
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
