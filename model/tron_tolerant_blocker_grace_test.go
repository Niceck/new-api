package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则 v3（站长 2026-10-18 定）：别的用户的订单只在它到期后 10 分钟内挡住别人的容差入账
// （v1/v2 是到期后 2 小时）。「对得上」的窗口本身不变：同一用户的几张单要算出同样的入账额度、
// 同一张单还有另一笔不精确款时转人工，这两道检查仍看到期后 2 小时。
const (
	bgBlockerGraceMS   int64 = 10 * 60 * 1000
	bgPlausibleGraceMS int64 = 2 * 60 * 60 * 1000
	bgBlockTimeMS      int64 = tolBaseMS + 10*60*60*1000
)

func TestTolerantSettle_OtherUsersOrderBlocksForTenMinutesAfterExpiry(t *testing.T) {
	require.Equal(t, bgBlockerGraceMS, tronTolerantBlockerGraceMS, "站长定的是 10 分钟")
	// 另一个用户的同档弃单（没付），在上链前 expiredAgoMS 到期。
	setup := func(t *testing.T, expiredAgoMS int64) *TronTopupOrder {
		tolOrder(t, "BG-other", 9600, 14_951_184, bgBlockTimeMS-expiredAgoMS-tolTTLMS, tolFaceQuota)
		return tolOrder(t, "BG-mine", 9601, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	}

	// 到期后恰好 10 分钟上链：仍挡（这笔钱也可能是他晚付的）。
	truncateTables(t)
	setup(t, bgBlockerGraceMS)
	requireTolUnmatched(t, tolTransfer(600, tolPaidMicros, bgBlockTimeMS), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9601))

	// 晚 1 毫秒：不再挡，按容差入账到我的单。
	truncateTablesNow(t)
	mine := setup(t, bgBlockerGraceMS+1)
	transfer := tolTransfer(601, tolPaidMicros, bgBlockTimeMS)
	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, mine.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9601))
	assert.Zero(t, tolQuota(t, 9600))
	assert.Equal(t, "v3", tolDeposit(t, transfer).MatchRule, "按新规则判定的款要记新版本号")
}

func TestTolerantSettle_AbandonedOrderAnHourEarlierNoLongerBlocks_RealTimeline(t *testing.T) {
	truncateTables(t)
	// 2026-10-18 那两笔的时间线，只把用户 528 的订单 21 换成没付的弃单：它 03:13:59 到期，
	// 531 的款 03:59:54 上链，晚了 46 分钟。v2 照样挡（在 2 小时内），v3 不再挡。
	tolOrder(t, "BG-528-abandoned", 9610, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	mine, transfer := pbMineAnHourLater(t, "BG-531", 9611, 610)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited, "别人一小时前的弃单不再挡")
	assert.Equal(t, mine.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9611), "多付按订单面值入账")
	assert.Zero(t, tolQuota(t, 9610))
	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronMatchModeTolerant, deposit.MatchMode)
	assert.Equal(t, "v3", deposit.MatchRule)
}

func TestTolerantSettle_OtherUsersOrderPastItsWindowIsIgnoredForUnderpayAndOtherFaceValues(t *testing.T) {
	// 窗口外的别人的订单在「几张单要算出同样的额度」那道检查里也不算：否则少付时两张单按比例
	// 算出的额度只差尾数、面值不同时更不一样，都会被误判成同一用户歧义转人工。
	pastWindow := bgBlockTimeMS - bgBlockerGraceMS - 1 - tolTTLMS

	// 少付：别人的同面值弃单。
	truncateTables(t)
	tolOrder(t, "BG-under-other", 9620, 14_951_184, pastWindow, tolFaceQuota)
	mine := tolOrder(t, "BG-under-mine", 9621, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(620, 14_500_000, bgBlockTimeMS), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, mine.ID, result.OrderID)
	want, err := tronSettlementQuota(tolExpectedMicros, 14_500_000, tolFaceQuota)
	require.NoError(t, err)
	assert.Equal(t, want, tolQuota(t, 9621), "少付按我这张单的比例入账")
	assert.Zero(t, tolQuota(t, 9620))

	// 面值不同：别人的 ¥110 档弃单，应付 16.446 USDT，仍在这笔款的 ±10% 内。
	truncateTablesNow(t)
	tolOrder(t, "BG-face-other", 9622, 16_446_000, pastWindow, 55_000_000)
	mine = tolOrder(t, "BG-face-mine", 9623, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	result, err = SettleTronDepositTolerant(tolTransfer(622, tolPaidMicros, bgBlockTimeMS), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, mine.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9623))
	assert.Zero(t, tolQuota(t, 9622))
}

func TestTolerantSettle_SameUsersOlderOrderStillCountsForTwoHours(t *testing.T) {
	require.Equal(t, bgPlausibleGraceMS, tronTolerantPlausibleGraceMS, "「对得上」的窗口不随 v3 缩短")
	// 同一用户一小时前到期的旧单（远超 10 分钟，仍在 2 小时内），面值不同：照旧转人工。
	truncateTables(t)
	tolOrder(t, "BG-own-old", 9630, 14_951_723, bgBlockTimeMS-60*60*1000-tolTTLMS, 55_000_000)
	tolOrder(t, "BG-own-live", 9630, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(630, tolPaidMicros, bgBlockTimeMS), nil, tronTolerantNoteSameUserAmbiguous)
	assert.Zero(t, tolQuota(t, 9630))

	// 到期超过 2 小时 1 毫秒的旧单不再算。
	truncateTablesNow(t)
	tolOrder(t, "BG-own-older", 9631, 14_951_723, bgBlockTimeMS-bgPlausibleGraceMS-1-tolTTLMS, 55_000_000)
	live := tolOrder(t, "BG-own-live2", 9631, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(631, tolPaidMicros, bgBlockTimeMS), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, live.ID, result.OrderID)
}

func TestTolerantSettle_AnotherInexactPaymentForTheOrderStillConflictsForTwoHours(t *testing.T) {
	// 同一张单的另一笔不精确款仍看到期后 2 小时：比如宽对账晚扫到我这笔时，库里已有一笔
	// 在我的单到期 1 小时后才上链的未匹配款。
	setup := func(t *testing.T) *TronTopupOrder {
		return tolOrder(t, "BG-rival-mine", 9640, tolExpectedMicros, bgBlockTimeMS-113_000, tolFaceQuota)
	}
	mineTransfer := tolTransfer(640, tolPaidMicros, bgBlockTimeMS)
	rivalAt := func(order *TronTopupOrder, afterExpiryMS int64) TronTransferRecord {
		return tolTransfer(641, 14_100_000, order.ExpiresAtMS+afterExpiryMS)
	}

	// 已记为未匹配的款，到期后 1 小时：照旧转人工。
	truncateTables(t)
	order := setup(t)
	_, err := RecordUnmatchedTronDeposit(rivalAt(order, 60*60*1000))
	require.NoError(t, err)
	requireTolUnmatched(t, mineTransfer, nil, tronTolerantNoteConflictDeposit)
	assert.Zero(t, tolQuota(t, 9640))

	// 同一批里还没记下的款，到期后 1 小时：同样转人工。
	truncateTablesNow(t)
	order = setup(t)
	requireTolUnmatched(t, mineTransfer, []TronTransferRecord{rivalAt(order, 60*60*1000)}, tronTolerantNoteConflictDeposit)

	// 到期超过 2 小时 1 毫秒的款不再算。
	truncateTablesNow(t)
	order = setup(t)
	_, err = RecordUnmatchedTronDeposit(rivalAt(order, bgPlausibleGraceMS+1))
	require.NoError(t, err)
	result, err := SettleTronDepositTolerant(mineTransfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, order.ID, result.OrderID)
}
