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
	// 按渠道点名：[渠道ID, ...]。这些渠道的上游在 chat/completions 的 usage 里会把值为 0 的
	// 明细对象整个省掉（prompt_tokens_details / completion_tokens_details），而 OpenAI 官方
	// 响应里它们始终存在。点名后，回给客户端的 usage 把缺的明细写明为 0。
	// 只改回给客户端的正文，不影响计费。只点名确认过「省略即为 0」的上游：对根本不报
	// 明细的上游，写 0 等于替它断言一个不知道的数。
	// 请求路径读的是 RefreshChatUsageDetailsFill 发布的快照。
	ChatUsageDetailsFillChannels []int `json:"chat_usage_details_fill_channels"`
	// 分组只能走指定的入口：{"分组": ["主机名", ...]}。登记了的分组，只有请求的 Host
	// （不看端口）在列表里才放行，别的入口一律回 403；没登记的分组不受限。
	// 用来把一条线路固定在它自己的接口地址上——第三方按接口地址的域名归并数据时，
	// 这条线路的记录不会混进主站域名名下。对所有客户端一视同仁，不看来源和请求形态。
	// 请求路径读的是 RefreshGroupEntryHosts 发布的快照。
	GroupEntryHosts map[string][]string `json:"group_entry_hosts"`
	// 分组走错入口时回给客户端的提示；空串用内置提示。可用占位符 {group}（分组名）
	// 与 {entry}（该分组登记的第一个主机名）。
	GroupEntryRejectMessage string `json:"group_entry_reject_message"`
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
	ChatUsageDetailsFillChannels: []int{},
	GroupEntryHosts:              map[string][]string{},
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
