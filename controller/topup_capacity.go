package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// topUpRequestAmountForQuota 把额度换回下单时用的数量单位（与钱包页输入框一致），向下取整。
// 按 tokens 展示时下单数量的单位就是额度，但入账仍按整数个充值单位，所以先取整再乘回去。
func topUpRequestAmountForQuota(quota int64) int64 {
	if quota <= 0 || common.QuotaPerUnit <= 0 {
		return 0
	}
	perUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	units := decimal.NewFromInt(quota).Div(perUnit).Floor()
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return units.Mul(perUnit).IntPart()
	}
	return units.IntPart()
}

// topUpWalletFullMessage 是下单被钱包上限挡住时给客户看的那句话：告诉他这次最多还能充多少；
// 连一笔最低充值都装不下时，告诉他先把余额用掉一些。minAmount 是该渠道的最低充值数量。
func topUpWalletFullMessage(full *model.TopUpWalletFullError, minAmount int64) string {
	maxAmount := topUpRequestAmountForQuota(full.Capacity.Room())
	message := fmt.Sprintf("充值后余额会超过钱包上限，本次最多可充 %d。", maxAmount)
	if maxAmount <= 0 || maxAmount < minAmount {
		message = "余额已接近钱包上限，暂时不能再充值，请先使用一部分余额。"
	}
	if full.Capacity.Pending > 0 {
		message += fmt.Sprintf("（另有 %d 笔未完成的充值订单占着额度，支付完成或过期后会释放。）", full.Capacity.Pending)
	}
	return message
}
