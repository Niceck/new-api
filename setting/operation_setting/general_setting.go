package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// 额度展示类型
const (
	QuotaDisplayTypeUSD    = "USD"
	QuotaDisplayTypeCNY    = "CNY"
	QuotaDisplayTypeTokens = "TOKENS"
	QuotaDisplayTypeCustom = "CUSTOM"
)

type GeneralSetting struct {
	DocsLink            string `json:"docs_link"`
	PingIntervalEnabled bool   `json:"ping_interval_enabled"`
	PingIntervalSeconds int    `json:"ping_interval_seconds"`
	// 当前站点额度展示类型：USD / CNY / TOKENS
	QuotaDisplayType string `json:"quota_display_type"`
	// 自定义货币符号，用于 CUSTOM 展示类型
	CustomCurrencySymbol string `json:"custom_currency_symbol"`
	// 自定义货币与美元汇率（1 USD = X Custom）
	CustomCurrencyExchangeRate float64 `json:"custom_currency_exchange_rate"`
	// 流式请求等上游响应头的上限，按渠道点名：{"渠道ID": 秒}。没列出的渠道不限时。
	// 超时后这次尝试按请求失败处理，走既有的重试。只对流式生效：非流式请求要等
	// 整段生成完才有响应头，套上限会误杀正常的长回复。
	StreamHeaderTimeoutByChannel map[string]int `json:"stream_header_timeout_by_channel"`
	// 一次请求连同各次重试，等响应头的总预算（秒）。0 = 不设总预算。
	// 用来保证「每次上限 × 尝试次数」不越过前置网关的超时（如 Cloudflare 的 100 秒）。
	// 请求路径不直接读这两个字段，读的是 RefreshStreamHeaderPolicy 发布的快照。
	StreamHeaderTotalBudgetSeconds int `json:"stream_header_total_budget_seconds"`
}

// 默认配置
var generalSetting = GeneralSetting{
	DocsLink:                     "https://docs.newapi.pro",
	PingIntervalEnabled:          false,
	PingIntervalSeconds:          60,
	QuotaDisplayType:             QuotaDisplayTypeUSD,
	CustomCurrencySymbol:         "¤",
	CustomCurrencyExchangeRate:   1.0,
	StreamHeaderTimeoutByChannel: map[string]int{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("general_setting", &generalSetting)
}

func GetGeneralSetting() *GeneralSetting {
	return &generalSetting
}

// IsCurrencyDisplay 是否以货币形式展示（美元或人民币）
func IsCurrencyDisplay() bool {
	return generalSetting.QuotaDisplayType != QuotaDisplayTypeTokens
}

// IsCNYDisplay 是否以人民币展示
func IsCNYDisplay() bool {
	return generalSetting.QuotaDisplayType == QuotaDisplayTypeCNY
}

// GetQuotaDisplayType 返回额度展示类型
func GetQuotaDisplayType() string {
	return generalSetting.QuotaDisplayType
}

// GetCurrencySymbol 返回当前展示类型对应符号
func GetCurrencySymbol() string {
	switch generalSetting.QuotaDisplayType {
	case QuotaDisplayTypeUSD:
		return "$"
	case QuotaDisplayTypeCNY:
		return "¥"
	case QuotaDisplayTypeCustom:
		if generalSetting.CustomCurrencySymbol != "" {
			return generalSetting.CustomCurrencySymbol
		}
		return "¤"
	default:
		return ""
	}
}

// GetUsdToCurrencyRate 返回 1 USD = X <currency> 的 X（TOKENS 不适用）
func GetUsdToCurrencyRate(usdToCny float64) float64 {
	switch generalSetting.QuotaDisplayType {
	case QuotaDisplayTypeUSD:
		return 1
	case QuotaDisplayTypeCNY:
		return usdToCny
	case QuotaDisplayTypeCustom:
		if generalSetting.CustomCurrencyExchangeRate > 0 {
			return generalSetting.CustomCurrencyExchangeRate
		}
		return 1
	default:
		return 1
	}
}
