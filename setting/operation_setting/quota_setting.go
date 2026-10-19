package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type QuotaSetting struct {
	RoundChargeToCNYCent      bool `json:"round_charge_to_cny_cent"`      // 每笔总费用按人民币分向上取整（默认关闭）
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
	// 余额与令牌都超过信任额度时免预扣、结束时一次扣全额（上游默认行为）。默认关闭 = 一律预扣：
	// 免预扣时在途请求不占余额，余额见底时的并发请求都能进来，欠费深度没有上限。
	TrustBypassEnabled bool `json:"trust_bypass_enabled"`
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}
