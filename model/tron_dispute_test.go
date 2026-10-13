package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// creditedToUserA stages a payment that tolerant matching credited to user A.
func tolCreditedToUserA(t *testing.T) (*TronTopupOrder, TronTransferRecord) {
	t.Helper()
	orderA := tolOrder(t, "DSP-A", 9201, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(200, tolPaidMicros, tolBaseMS+113_000)
	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	require.True(t, result.Credited)
	return orderA, transfer
}

func TestTronDispute_OtherUsersClaimOnCreditedTransactionOpensDispute(t *testing.T) {
	truncateTables(t)
	_, transfer := tolCreditedToUserA(t)
	// 用户 B 在这笔款入账之后才下的单会早于付款时间吗？不会——所以让 B 的单建在付款之前，
	// 但晚于阻断窗口（过期超过 2 小时）而仍在 24 小时申诉期内：这正是会被错归属的那类迟到款。
	orderB := tolOrder(t, "DSP-B", 9202, 14_951_184, tolBaseMS-tolTTLMS-tronTolerantBlockerGraceMS-60_000, tolFaceQuota)
	claimAt := tolBaseMS + 30*60*1000

	ticket, err := submitTronTopupClaimAt(9202, orderB.TradeNo, transfer.TxID, "this was my payment", claimAt)
	require.NoError(t, err)
	assert.Equal(t, TronTicketReasonDispute, ticket.Reason)
	assert.Equal(t, TronTicketStatusOpen, ticket.Status)
	assert.Equal(t, 9202, ticket.UserID)
	assert.Equal(t, orderB.ID, ticket.OrderID)
	require.NotNil(t, ticket.DepositID)
	assert.Equal(t, tolDeposit(t, transfer).ID, *ticket.DepositID)
	assert.Zero(t, ticket.CreditedUserID, "争议工单不透露已入账的账户")

	assert.Equal(t, tolFaceQuota, tolQuota(t, 9201), "提交争议不动任何人的余额")
	assert.Zero(t, tolQuota(t, 9202))
	assert.Equal(t, TronDepositStatusCredited, tolDeposit(t, transfer).Status)

	again, err := submitTronTopupClaimAt(9202, orderB.TradeNo, transfer.TxID, "retry", claimAt+1_000)
	require.NoError(t, err)
	assert.Equal(t, ticket.ID, again.ID, "重复提交返回同一张争议工单")
	var disputes int64
	require.NoError(t, DB.Model(&TronTopupTicket{}).Where("reason = ?", TronTicketReasonDispute).Count(&disputes).Error)
	assert.Equal(t, int64(1), disputes)
}

func TestTronDispute_CannotCreditAndIsClosedByRejecting(t *testing.T) {
	truncateTables(t)
	_, transfer := tolCreditedToUserA(t)
	orderB := tolOrder(t, "DSP-B2", 9203, 14_951_184, tolBaseMS-tolTTLMS-tronTolerantBlockerGraceMS-60_000, tolFaceQuota)
	ticket, err := submitTronTopupClaimAt(9203, orderB.TradeNo, transfer.TxID, "mine", tolBaseMS+30*60*1000)
	require.NoError(t, err)

	err = ResolveTronTopupTicket(ticket.ID, orderB.ID, transfer, 1, "try to credit a dispute")
	require.ErrorIs(t, err, ErrTronDisputeNotResolvable)
	assert.Zero(t, tolQuota(t, 9203), "普通通过接口不能给争议工单加款")
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9201))

	require.NoError(t, RejectTronTopupTicket(ticket.ID, 1, "已核实并线下处理", tolBaseMS+40*60*1000))
	var closed TronTopupTicket
	require.NoError(t, DB.First(&closed, ticket.ID).Error)
	assert.Equal(t, TronTicketStatusRejected, closed.Status)
	assert.Equal(t, "已核实并线下处理", closed.AdminNote)
}

func TestTronDispute_SameUserOrImplausibleClaimantGetsTheOldAnswer(t *testing.T) {
	truncateTables(t)
	_, transfer := tolCreditedToUserA(t)

	// 同一用户拿自己已入账的交易再申诉：不是争议。
	another := tolOrder(t, "DSP-A-again", 9201, 14_960_000, tolBaseMS+30*60*1000, tolFaceQuota)
	_, err := submitTronTopupClaimAt(9201, another.TradeNo, transfer.TxID, "again", tolBaseMS+31*60*1000)
	require.ErrorIs(t, err, ErrTronTxAlreadyClaimed)

	// 金额对不上的订单：不能拿公开的 txid 来占一张争议工单。
	otherTier := tolOrder(t, "DSP-C-tier", 9204, 74_582_856, tolBaseMS-60_000, 250_000_000)
	_, err = submitTronTopupClaimAt(9204, otherTier.TradeNo, transfer.TxID, "not mine", tolBaseMS+31*60*1000)
	require.ErrorIs(t, err, ErrTronTxAlreadyClaimed)

	// 付款之后才建的订单：同样不行。
	later := tolOrder(t, "DSP-D-later", 9205, 14_951_184, tolBaseMS+10*60*1000, tolFaceQuota)
	_, err = submitTronTopupClaimAt(9205, later.TradeNo, transfer.TxID, "not mine", tolBaseMS+31*60*1000)
	require.ErrorIs(t, err, ErrTronTxAlreadyClaimed)

	var tickets int64
	require.NoError(t, DB.Model(&TronTopupTicket{}).Count(&tickets).Error)
	assert.Zero(t, tickets)
}

func TestResolveTronTicket_ProratesUnderpayAndRecordsManualMatch(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "RSV-under", 9210, 10_000_000, tolBaseMS, 10_000_000)
	// 实到是应付的 95%，订单到期之后才到账 → 扫描器不会自动处理，走人工。
	transfer := tolTransfer(210, 9_500_000, tolBaseMS+tolTTLMS+60_000)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	ticket, err := submitTronTopupClaimAt(9210, order.TradeNo, transfer.TxID, "exchange fee", tolBaseMS+tolTTLMS+120_000)
	require.NoError(t, err)

	require.NoError(t, ResolveTronTopupTicket(ticket.ID, order.ID, transfer, 1, "verified"))
	assert.Equal(t, 9_500_000, tolQuota(t, 9210), "少付按比例入账")
	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronDepositStatusCredited, deposit.Status)
	assert.Equal(t, 9_500_000, deposit.CreditedQuota)
	assert.Equal(t, TronMatchModeManual, deposit.MatchMode)
	assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order))
}

func TestCreateOrGetActiveTronTopupOrderAt_StampsTheOrderAfterTakingTheLedgerLock(t *testing.T) {
	truncateTables(t)
	tolUser(t, 9220, 0)
	provisional := tolBaseMS
	topUp := &TopUp{UserId: 9220, Amount: 100, Money: 100, TradeNo: "CLK-1", PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: provisional / 1000, Status: common.TopUpStatusPending}
	order := &TronTopupOrder{UserID: 9220, TradeNo: "CLK-1", ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: tolExpectedMicros, RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: provisional, ExpiresAtMS: provisional + tolTTLMS, CreditQuota: tolFaceQuota, CreatedAtMS: provisional}
	// 请求在取报价、等锁上耽搁了 45 秒：订单的创建时间必须是拿到锁之后的时间，
	// 不能让它看起来比真正落库早 45 秒（那会抵掉「至少早于付款 60 秒」这道门槛）。
	lockedAt := provisional + 45_000

	stored, created, err := CreateOrGetActiveTronTopupOrderAt(topUp, order, func() int64 { return lockedAt })
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, lockedAt, stored.CreatedAtMS)
	assert.Equal(t, lockedAt+tolTTLMS, stored.ExpiresAtMS, "有效期长度不变")

	var persisted TronTopupOrder
	require.NoError(t, DB.Where("trade_no = ?", "CLK-1").First(&persisted).Error)
	assert.Equal(t, lockedAt, persisted.CreatedAtMS)
	assert.Equal(t, lockedAt+tolTTLMS, persisted.ExpiresAtMS)
	var persistedTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "CLK-1").First(&persistedTopUp).Error)
	assert.Equal(t, lockedAt/1000, persistedTopUp.CreateTime)

	// 已有有效订单时直接复用：传进来的草稿对象原样还回，不留下被改过的时间。
	draftTopUp := &TopUp{UserId: 9220, Amount: 100, Money: 100, TradeNo: "CLK-2", PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: provisional / 1000, Status: common.TopUpStatusPending}
	draft := &TronTopupOrder{UserID: 9220, TradeNo: "CLK-2", ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: tolExpectedMicros + 1, RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: provisional, ExpiresAtMS: provisional + tolTTLMS, CreditQuota: tolFaceQuota, CreatedAtMS: provisional}
	reused, created, err := CreateOrGetActiveTronTopupOrderAt(draftTopUp, draft, func() int64 { return lockedAt + 5_000 })
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, "CLK-1", reused.TradeNo)
	assert.Equal(t, provisional, draft.CreatedAtMS)
	assert.Equal(t, provisional+tolTTLMS, draft.ExpiresAtMS)
	assert.Equal(t, provisional/1000, draftTopUp.CreateTime)
}

func TestTronSettlementView_ReportsWhatActuallyArrivedAndWasCredited(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "VIEW-under", 9230, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	pending, err := GetTronOrderSettlement(order.ID)
	require.NoError(t, err)
	assert.Nil(t, pending, "没入账的订单没有结算信息")

	transfer := tolTransfer(230, 13_952_525, tolBaseMS+300_000)
	_, err = SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	settlement, err := GetTronOrderSettlement(order.ID)
	require.NoError(t, err)
	require.NotNil(t, settlement)
	assert.Equal(t, int64(13_952_525), settlement.ReceivedMicros)
	assert.Equal(t, 46_656_083, settlement.CreditedQuota)
	assert.Equal(t, TronMatchModeTolerant, settlement.MatchMode)

	minMicros, maxMicros := TronTolerantAmountRange(tolExpectedMicros)
	assert.Equal(t, int64(13_457_273), minMicros, "90% 向上取整：再少一个微单位就不在范围内")
	assert.Equal(t, int64(16_447_777), maxMicros, "110% 向下取整")
	assert.True(t, tronAmountWithinPolicy(tolExpectedMicros, minMicros))
	assert.False(t, tronAmountWithinPolicy(tolExpectedMicros, minMicros-1))
	assert.True(t, tronAmountWithinPolicy(tolExpectedMicros, maxMicros))
	assert.False(t, tronAmountWithinPolicy(tolExpectedMicros, maxMicros+1))

	// 充值记录：订单面值与应付保持原值，另外带出实到与实际入账。
	tolOrder(t, "VIEW-unpaid", 9230, 14_960_000, tolBaseMS+30*60*1000, tolFaceQuota)
	topUps, total, err := GetUserTopUps(9230, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	byTradeNo := map[string]*TopUp{}
	for _, topUp := range topUps {
		byTradeNo[topUp.TradeNo] = topUp
	}
	paid := byTradeNo["VIEW-under"]
	require.NotNil(t, paid)
	assert.Equal(t, int64(100), paid.Amount)
	assert.Equal(t, int64(13_952_525), paid.TronReceivedMicros)
	assert.Equal(t, 46_656_083, paid.TronCreditedQuota)
	assert.Equal(t, tolFaceQuota, paid.TronFaceQuota)
	unpaid := byTradeNo["VIEW-unpaid"]
	require.NotNil(t, unpaid)
	assert.Zero(t, unpaid.TronReceivedMicros)
	assert.Zero(t, unpaid.TronCreditedQuota)
}
