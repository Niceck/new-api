package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// PrepareBillingCharge snapshots the policy, without committing a debit.
func PrepareBillingCharge(info *relaycommon.RelayInfo, raw int) (*common.BillingCharge, error) {
	if info == nil {
		return nil, fmt.Errorf("relayInfo is nil")
	}
	if info.BillingRounding == nil {
		info.BillingRounding = &common.CNYCentRounding{Enabled: operation_setting.GetQuotaSetting().RoundChargeToCNYCent, USDToCNY: operation_setting.USDExchangeRate, QuotaPerUnit: common.QuotaPerUnit}
	}
	charged, err := info.BillingRounding.Charge(raw)
	if err != nil {
		if clamp, ok := err.(*common.QuotaClamp); ok {
			noteQuotaClamp(info, clamp)
		}
		common.SysError("CNY charge calculation rejected: " + err.Error())
		return nil, err
	}
	return &common.BillingCharge{RawQuota: raw, ChargedQuota: charged, Policy: *info.BillingRounding}, nil
}

func ChargedQuota(info *relaycommon.RelayInfo, raw int) int {
	if info != nil && info.BillingCharge != nil {
		return info.BillingCharge.ChargedQuota
	}
	return raw
}

// AttachBillingCharge leaves the original quota/price fields untouched.
func AttachBillingCharge(info *relaycommon.RelayInfo, other map[string]interface{}) {
	if info != nil && info.BillingCharge != nil && info.BillingCharge.Policy.Enabled && other != nil {
		other["charge_rounding"] = info.BillingCharge.AuditMap()
	}
}

// ConsumeStandaloneQuota is for independent fees without a billing session.
// PostConsumeQuota remains a raw delta primitive for refunds and adjustments.
func ConsumeStandaloneQuota(info *relaycommon.RelayInfo, raw int, notify bool) error {
	charge, err := PrepareBillingCharge(info, raw)
	if err != nil {
		return err
	}
	if err = PostConsumeQuota(info, charge.ChargedQuota, 0, notify); err != nil {
		return err
	}
	info.BillingCharge = charge
	return nil
}

// settleRoundedTask preserves raw log deltas even when two totals round to the
// same cent. The model transaction persists the marker alongside the debit.
func settleRoundedTask(ctx context.Context, task *model.Task, raw int, refund bool, reason string, clamps ...*common.QuotaClamp) bool {
	old, changed, err := model.SettleRoundedTaskQuota(task, raw, refund)
	if err != nil {
		logger.LogError(ctx, "CNY task settlement failed: "+err.Error())
		return false
	}
	if !changed {
		return true
	}
	current := task.PrivateData.BillingCharge
	rawDelta := current.RawQuota - old.RawQuota
	chargedDelta := current.ChargedQuota - old.ChargedQuota
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	other["charge_rounding"] = current.AuditMap()
	other["charge_rounding_previous"] = old.AuditMap()
	other["charged_delta"] = chargedDelta
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	logType := model.LogTypeConsume
	if rawDelta < 0 || refund {
		logType = model.LogTypeRefund
		rawDelta = -rawDelta
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: task.UserId, LogType: logType, Content: reason, ChannelId: task.ChannelId, ModelName: taskModelName(task), Quota: rawDelta, TokenId: task.PrivateData.TokenId, Group: task.Group, Other: other, NodeName: task.PrivateData.NodeName})
	return true
}

// InsertMidjourneyTask keeps accepted upstream work trackable even if local
// billing fails: a rolled-back debit is recorded as zero with an audit marker.
func InsertMidjourneyTask(info *relaycommon.RelayInfo, task *model.Midjourney, raw int, bill bool) error {
	task.Quota = 0
	if !bill {
		return task.Insert()
	}
	charge, err := PrepareBillingCharge(info, raw)
	if err != nil {
		return err
	}
	task.Quota = charge.ChargedQuota
	task.BillingCharge = charge
	if !info.IsPlayground {
		task.TokenId = info.TokenId
	}
	if err := model.InsertChargedMidjourney(task); err != nil {
		common.SysError(fmt.Sprintf("MJ local billing rolled back, upstream task %s: %s", task.MjId, err))
		task.Id = 0
		task.Quota = 0
		charge.ChargedQuota = 0
		charge.SettlementError = "local_billing_failed"
		if insertErr := task.Insert(); insertErr != nil {
			return insertErr
		}
	}
	info.BillingCharge = charge
	if charge.ChargedQuota > 0 {
		checkAndSendQuotaNotify(info, charge.ChargedQuota, 0)
	}
	return nil
}
