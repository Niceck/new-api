package operation_setting

import (
	"encoding/json" // 只引用 json.Number 这个类型；解析走 common.UnmarshalJsonStr
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// 充值赠送（2026-11-02 站长定）：充值优惠从「少付钱」改成「实付等于面值、多送一定比例的额度」。
//
//   - payment_setting.amount_bonus：单笔满额赠送，{"200":0.025,"500":0.035,...}。按「满额」取档，600 按 500 那档算。
//   - payment_setting.tier_bonus：累计等级赠送，{"1000":0.025,...}。累计到账面值（不含赠送）达档后每笔都享。
//   - 两者取高、不叠加。赠送额在建单时算好落进 top_ups.bonus_quota，入账只认这个快照，之后改配置不影响在途订单。
//
// amount_bonus 非空就是「赠送模式」：折扣表不再生效，实付恒等于面值，所以不会出现既打折又赠送的订单。
// 它是唯一的开关：等级表只在开关打开时才生效。切换时先写好等级表、再写单笔表，写单笔表的那一下整体切换，
// 任何时刻一笔订单要么全按折扣、要么全按赠送。写 {} 即关闭，关闭后折扣表（amount_discount / tier_discount）恢复生效。

const (
	// AmountBonusOptionKey / TierBonusOptionKey 配置键；写入前必须过 ValidateTopUpBonusJSON（模型层与管理接口都挂了）。
	AmountBonusOptionKey = "payment_setting.amount_bonus"
	TierBonusOptionKey   = "payment_setting.tier_bonus"

	// 赠送比例上限：一半。再大多半是把折扣（0.92）写到了这里。
	maxTopUpBonusRate = 0.5
	// 比例最多四位小数：QuotaPerUnit 为 500000 时，整数面值乘四位小数恒为整数额度，不用取舍。
	topUpBonusDecimals = 4
)

// ErrTopUpBonusInvalid 配置不合法时的统一前缀，便于后台与运维脚本按前缀识别。
var ErrTopUpBonusInvalid = errors.New("充值赠送配置不合法")

// bonusStepsFromMap 把配置表整理成按门槛升序的台阶。任何一项不合法就整体报错，不悄悄丢掉。
func bonusStepsFromMap(raw map[int]float64) ([]TopUpTier, error) {
	if len(raw) > maxTopUpTiers {
		return nil, fmt.Errorf("%w：最多 %d 档，现在有 %d 档", ErrTopUpBonusInvalid, maxTopUpTiers, len(raw))
	}
	steps := make([]TopUpTier, 0, len(raw))
	for threshold, rate := range raw {
		if threshold <= 0 || threshold > maxTopUpTierThreshold {
			return nil, fmt.Errorf("%w：门槛必须是 1 到 %d 之间的整数，收到 %d", ErrTopUpBonusInvalid, maxTopUpTierThreshold, threshold)
		}
		if !(rate > 0 && rate <= maxTopUpBonusRate) {
			return nil, fmt.Errorf("%w：门槛 %d 的比例 %v 必须大于 0 且不超过 %v（0.035 表示多送 3.5%%）", ErrTopUpBonusInvalid, threshold, rate, maxTopUpBonusRate)
		}
		if scaled := rate * 10000; math.Abs(scaled-math.Round(scaled)) > 1e-7 {
			return nil, fmt.Errorf("%w：门槛 %d 的比例 %v 最多四位小数", ErrTopUpBonusInvalid, threshold, rate)
		}
		steps = append(steps, TopUpTier{Threshold: int64(threshold), Discount: 1, Bonus: rate})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Threshold < steps[j].Threshold })
	for i := range steps {
		steps[i].Level = i + 1
		if i > 0 && steps[i].Bonus <= steps[i-1].Bonus {
			// 必须严格递增：多充反而少送说不过去；反求「最多可充」也依赖面值与到账同向增长
			return nil, fmt.Errorf("%w：门槛 %d 的比例 %v 不高于门槛 %d 的 %v，越高档应送得越多",
				ErrTopUpBonusInvalid, steps[i].Threshold, steps[i].Bonus, steps[i-1].Threshold, steps[i-1].Bonus)
		}
	}
	return steps, nil
}

// ValidateTopUpBonusJSON 写入两张赠送表前的严格校验（模型层与管理接口共用）：
// 必须是 {"正整数门槛": 比例} 的 JSON 对象，门槛不带前导零 / 符号 / 指数且不重复，
// 比例按十进制解析、在 (0, 0.5] 内且最多四位小数，越高档送得越多，最多 20 档；
// 按 tokens 展示的站点不允许开启（各支付渠道订单面值的单位不一致）。
func ValidateTopUpBonusJSON(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%w：不能为空，要关闭赠送请写 {}", ErrTopUpBonusInvalid)
	}
	var raw map[string]json.Number
	if err := common.UnmarshalJsonStr(trimmed, &raw); err != nil {
		return fmt.Errorf("%w：应为 {\"门槛\": 比例} 的 JSON 对象（%v）", ErrTopUpBonusInvalid, err)
	}
	if raw == nil {
		return fmt.Errorf("%w：不能是 null，要关闭赠送请写 {}", ErrTopUpBonusInvalid)
	}
	// json.Number 接受带引号的数字，配置框架解析 map[int]float64 时却会失败并静默跳过：这里必须拦住
	var shapes map[string]any
	if err := common.UnmarshalJsonStr(trimmed, &shapes); err != nil {
		return fmt.Errorf("%w：应为 {\"门槛\": 比例} 的 JSON 对象（%v）", ErrTopUpBonusInvalid, err)
	}
	for key, shape := range shapes {
		if _, isNumber := shape.(float64); !isNumber {
			return fmt.Errorf("%w：门槛 %s 的比例必须是数字", ErrTopUpBonusInvalid, key)
		}
	}
	if keys := tierKeyPattern.FindAllStringIndex(trimmed, -1); len(keys) != len(raw) {
		return fmt.Errorf("%w：有重复的门槛键", ErrTopUpBonusInvalid)
	}
	if len(raw) > maxTopUpTiers {
		return fmt.Errorf("%w：最多 %d 档，现在有 %d 档", ErrTopUpBonusInvalid, maxTopUpTiers, len(raw))
	}
	maxRate := decimal.NewFromFloat(maxTopUpBonusRate)
	table := make(map[int]float64, len(raw))
	for key, number := range raw {
		threshold, err := strconv.ParseInt(key, 10, 64)
		if err != nil || strconv.FormatInt(threshold, 10) != key {
			return fmt.Errorf("%w：门槛 %q 必须是不带前导零、符号与指数的正整数", ErrTopUpBonusInvalid, key)
		}
		if threshold <= 0 || threshold > maxTopUpTierThreshold {
			return fmt.Errorf("%w：门槛必须是 1 到 %d 之间的整数，收到 %d", ErrTopUpBonusInvalid, maxTopUpTierThreshold, threshold)
		}
		if _, dup := table[int(threshold)]; dup {
			return fmt.Errorf("%w：门槛 %d 重复", ErrTopUpBonusInvalid, threshold)
		}
		rate, err := decimal.NewFromString(number.String())
		if err != nil || !rate.IsPositive() || rate.GreaterThan(maxRate) {
			return fmt.Errorf("%w：门槛 %d 的比例 %s 必须大于 0 且不超过 %s（0.035 表示多送 3.5%%）", ErrTopUpBonusInvalid, threshold, number.String(), maxRate.String())
		}
		if !rate.Equal(rate.Round(topUpBonusDecimals)) {
			return fmt.Errorf("%w：门槛 %d 的比例 %s 最多四位小数", ErrTopUpBonusInvalid, threshold, number.String())
		}
		table[int(threshold)] = rate.InexactFloat64()
	}
	if _, err := bonusStepsFromMap(table); err != nil {
		return err
	}
	if len(table) > 0 && GetQuotaDisplayType() == QuotaDisplayTypeTokens {
		return fmt.Errorf("%w：按 tokens 展示时各支付渠道订单面值的单位不一致，充值赠送不支持；请先改回按货币展示", ErrTopUpBonusInvalid)
	}
	return nil
}

var (
	badBonusConfigMu   sync.Mutex
	badBonusConfigSeen string
)

// noteBadBonusConfig 运行态发现配置不合法（本不该写得进来，只可能是直接改库）：按没有赠送处理，
// 但要记一条错误日志，别静默成「没配置」。同一份坏配置只记一次。
func noteBadBonusConfig(raw map[int]float64, err error) {
	fingerprint := fmt.Sprintf("%v", raw) // map 的打印是按键排序的，可当指纹
	badBonusConfigMu.Lock()
	defer badBonusConfigMu.Unlock()
	if badBonusConfigSeen == fingerprint {
		return
	}
	badBonusConfigSeen = fingerprint
	common.SysError("充值赠送配置不合法，已按没有赠送处理（客户付面值、到账面值）：" + err.Error())
}

func bonusSteps(raw map[int]float64) []TopUpTier {
	steps, err := bonusStepsFromMap(raw)
	if err != nil {
		noteBadBonusConfig(raw, err)
		return nil
	}
	if len(steps) == 0 {
		return nil
	}
	return steps
}

// AmountBonusSteps 单笔满额赠送的台阶（按门槛升序）。没配置或配置不合法返回 nil。
func AmountBonusSteps() []TopUpTier {
	return bonusSteps(GetPaymentSetting().AmountBonus)
}

// TopUpBonusTiers 累计等级赠送的等级表（按门槛升序）。没配置或配置不合法返回 nil。
func TopUpBonusTiers() []TopUpTier {
	return bonusSteps(GetPaymentSetting().TierBonus)
}

// AmountBonusRate 这个面值的单笔赠送比例：取不超过面值的最高一档，没有则为 0。
func AmountBonusRate(amount int64) float64 {
	rate := 0.0
	for _, step := range AmountBonusSteps() {
		if amount < step.Threshold {
			break
		}
		rate = step.Bonus
	}
	return rate
}

// TopUpBonusConfigured 两张赠送表有没有写过内容（不看展示类型），切 tokens 展示前的检查用。
func TopUpBonusConfigured() bool {
	return len(AmountBonusSteps()) > 0 || len(TopUpBonusTiers()) > 0
}

// TopUpBonusEnabled 现在是不是赠送模式：单笔赠送表非空就是，它是唯一的开关（见文件头）。
// 按 tokens 展示时不生效：写入时已经拒绝，这里兜住先配好再切展示的情形。
func TopUpBonusEnabled() bool {
	return GetQuotaDisplayType() != QuotaDisplayTypeTokens && len(AmountBonusSteps()) > 0
}
