package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 这些用例只在真实 MySQL 上有意义：SQLite 没有行锁，测试库又把连接数限成 1，
// 并发被连接池串行化，去掉任何一把锁都照样通过。用法见 topup_epay_mysql_test.go。

func withTronTestMySQL(t *testing.T, userIDs []int, fn func()) {
	t.Helper()
	withEpayTestMySQL(t, func() {
		require.NoError(t, DB.AutoMigrate(&TronTopupOrder{}, &TronDeposit{}, &TronTopupTicket{}, &TronSerialLock{}))
		require.NoError(t, EnsureTronSerialLock())
		clean := func() {
			for _, userID := range userIDs {
				var orders []TronTopupOrder
				require.NoError(t, DB.Where("user_id = ?", userID).Find(&orders).Error)
				for _, order := range orders {
					require.NoError(t, DB.Where("order_id = ?", order.ID).Delete(&TronDeposit{}).Error)
					require.NoError(t, DB.Where("order_id = ?", order.ID).Delete(&TronTopupTicket{}).Error)
				}
				require.NoError(t, DB.Where("user_id = ?", userID).Delete(&TronTopupOrder{}).Error)
				require.NoError(t, DB.Unscoped().Where("user_id = ?", userID).Delete(&TopUp{}).Error)
				require.NoError(t, DB.Unscoped().Where("user_id = ?", userID).Delete(&Log{}).Error)
				require.NoError(t, DB.Unscoped().Where("id = ?", userID).Delete(&User{}).Error)
			}
			// 没抢到订单的款 order_id 为空，按测试专用的 txid 段清掉（60 个 0 加 9xxx）。
			require.NoError(t, DB.Where("tx_id LIKE ?", strings.Repeat("0", 60)+"9%").Delete(&TronDeposit{}).Error)
		}
		clean()
		t.Cleanup(clean)
		fn()
	})
}

// 每个用例用自己的金额档，互不落进对方的识别范围，可以反复跑（-count=N）。
func mysqlTolOrder(t *testing.T, tradeNo string, userID int, expectedMicros int64, createdMS int64) *TronTopupOrder {
	t.Helper()
	return tolOrder(t, fmt.Sprintf("%s-%d", tradeNo, time.Now().UnixNano()), userID, expectedMicros, createdMS, tolFaceQuota)
}

func TestTronTolerantMySQL_ConcurrentSameTransactionCreditsOnce(t *testing.T) {
	const userID = 940001
	withTronTestMySQL(t, []int{userID}, func() {
		order := mysqlTolOrder(t, "tol-mysql-same", userID, 21_000_000, tolBaseMS)
		transfer := tolTransfer(0x9001, 21_100_000, tolBaseMS+113_000)

		const callers = 12
		results := make([]TronSettlementResult, callers)
		errs := make([]error, callers)
		var wg, gate sync.WaitGroup
		gate.Add(1)
		wg.Add(callers)
		for i := 0; i < callers; i++ {
			go func(idx int) {
				defer wg.Done()
				gate.Wait()
				results[idx], errs[idx] = SettleTronDepositTolerant(transfer, nil, "tron-scanner")
			}(i)
		}
		gate.Done()
		wg.Wait()

		first := 0
		for i := 0; i < callers; i++ {
			require.NoError(t, errs[i], "并发重扫不得报错")
			assert.True(t, results[i].Credited)
			assert.Equal(t, order.ID, results[i].OrderID)
			if !results[i].AlreadyProcessed {
				first++
			}
		}
		assert.Equal(t, 1, first, "只有一个调用真正入账，其余看到的是已处理")
		assert.Equal(t, tolFaceQuota, userQuota(t, userID))
		var deposits, logs int64
		require.NoError(t, DB.Model(&TronDeposit{}).Where("tx_id = ?", transfer.TxID).Count(&deposits).Error)
		require.NoError(t, DB.Model(&Log{}).Where("user_id = ? AND type = ?", userID, LogTypeTopup).Count(&logs).Error)
		assert.EqualValues(t, 1, deposits)
		assert.EqualValues(t, 1, logs)
	})
}

func TestTronTolerantMySQL_TwoPaymentsRacingForOneOrderCreditItOnce(t *testing.T) {
	const userID = 940002
	withTronTestMySQL(t, []int{userID}, func() {
		order := mysqlTolOrder(t, "tol-mysql-race", userID, 31_000_000, tolBaseMS)
		transfers := []TronTransferRecord{
			tolTransfer(0x9002, 31_200_000, tolBaseMS+113_000),
			tolTransfer(0x9003, 30_100_000, tolBaseMS+150_000),
		}
		results := make([]TronSettlementResult, len(transfers))
		errs := make([]error, len(transfers))
		var wg, gate sync.WaitGroup
		gate.Add(1)
		wg.Add(len(transfers))
		for i := range transfers {
			go func(idx int) {
				defer wg.Done()
				gate.Wait()
				results[idx], errs[idx] = SettleTronDepositTolerant(transfers[idx], nil, "tron-scanner")
			}(i)
		}
		gate.Done()
		wg.Wait()

		credited := 0
		for i := range transfers {
			require.NoError(t, errs[i])
			if results[i].Credited {
				credited++
				assert.Equal(t, order.ID, results[i].OrderID)
			} else {
				assert.True(t, results[i].Unmatched, "没抢到订单的那笔留给人工")
			}
		}
		assert.Equal(t, 1, credited, "一张订单只能成功一次")
		quota := userQuota(t, userID)
		assert.Contains(t, []int{tolFaceQuota, 48_548_387}, quota, "入账的是其中一笔的额度，不是两笔之和")
		assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order))
	})
}

func TestTronTolerantMySQL_SettlementWaitsForTheLedgerLock(t *testing.T) {
	const userID = 940003
	withTronTestMySQL(t, []int{userID}, func() {
		mysqlTolOrder(t, "tol-mysql-serial", userID, 41_000_000, tolBaseMS)
		transfer := tolTransfer(0x9004, 41_100_000, tolBaseMS+113_000)

		const hold = 600 * time.Millisecond
		holding := make(chan struct{})
		released := make(chan time.Time, 1)
		go func() {
			_ = DB.Transaction(func(tx *gorm.DB) error {
				if err := lockTronSerialTx(tx); err != nil {
					return err
				}
				close(holding)
				time.Sleep(hold)
				released <- time.Now()
				return nil
			})
		}()
		<-holding

		result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
		finished := time.Now()
		require.NoError(t, err)
		assert.True(t, result.Credited)
		assert.False(t, finished.Before(<-released), "别的账本事务持有串行锁期间，结算必须等待")
	})
}

func TestTronTolerantMySQL_TronAndEpayCreditTheSameUserWithoutLoss(t *testing.T) {
	const userID = 940004
	withTronTestMySQL(t, []int{userID}, func() {
		const epayTradeNo = "tol-mysql-epay"
		require.NoError(t, DB.Unscoped().Where("trade_no = ?", epayTradeNo).Delete(&TopUp{}).Error)
		epay := epayFixture(t, epayTradeNo, userID, 10, 0)
		mysqlTolOrder(t, "tol-mysql-mix", userID, 51_000_000, tolBaseMS)
		transfer := tolTransfer(0x9005, 51_100_000, tolBaseMS+113_000)

		var wg, gate sync.WaitGroup
		gate.Add(1)
		wg.Add(2)
		var tronErr, epayErr error
		go func() {
			defer wg.Done()
			gate.Wait()
			_, tronErr = SettleTronDepositTolerant(transfer, nil, "tron-scanner")
		}()
		go func() {
			defer wg.Done()
			gate.Wait()
			_, epayErr = RechargeEpay(epay.TradeNo, "alipay", "10.00", "127.0.0.1")
		}()
		gate.Done()
		wg.Wait()

		require.NoError(t, tronErr, "两条入账通道并发不得死锁或报错")
		require.NoError(t, epayErr)
		assert.Equal(t, tolFaceQuota+int(decimalQuotaForAmount(10)), userQuota(t, userID), "两笔都要入账，一分不丢")
	})
}

func TestTronTolerantMySQL_OrderCreationAndSettlementInterleaveSafely(t *testing.T) {
	const payerID = 940005
	creators := []int{940006, 940007, 940008, 940009}
	withTronTestMySQL(t, append([]int{payerID}, creators...), func() {
		order := mysqlTolOrder(t, "tol-mysql-interleave", payerID, 61_000_000, tolBaseMS)
		transfer := tolTransfer(0x9006, 61_100_000, tolBaseMS+113_000)
		for _, creator := range creators {
			tolUser(t, creator, 0)
		}

		var wg, gate sync.WaitGroup
		gate.Add(1)
		errs := make([]error, len(creators)+1)
		wg.Add(len(creators) + 1)
		for i, creator := range creators {
			go func(idx int, userID int) {
				defer wg.Done()
				gate.Wait()
				// 别的用户同时建远离这个金额档的订单：不构成阻断，只考验锁顺序。
				createdMS := tolBaseMS + int64(idx)
				tradeNo := fmt.Sprintf("tol-mysql-creator-%d-%d", userID, time.Now().UnixNano())
				topUp := &TopUp{UserId: userID, Amount: 100, Money: 100, TradeNo: tradeNo, PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: createdMS / 1000, Status: common.TopUpStatusPending}
				draft := &TronTopupOrder{UserID: userID, TradeNo: tradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: 81_000_000 + int64(idx), RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: createdMS, ExpiresAtMS: createdMS + tolTTLMS, CreditQuota: tolFaceQuota, CreatedAtMS: createdMS}
				_, _, errs[idx] = CreateOrGetActiveTronTopupOrderAt(topUp, draft, func() int64 { return createdMS })
			}(i, creator)
		}
		go func() {
			defer wg.Done()
			gate.Wait()
			_, errs[len(creators)] = SettleTronDepositTolerant(transfer, nil, "tron-scanner")
		}()
		gate.Done()
		wg.Wait()

		for i, err := range errs {
			require.NoError(t, err, "第 %d 个并发调用不得死锁或报错", i)
		}
		assert.Equal(t, tolFaceQuota, userQuota(t, payerID))
		assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order))
		var created int64
		require.NoError(t, DB.Model(&TronTopupOrder{}).Where("user_id IN ?", creators).Count(&created).Error)
		assert.EqualValues(t, len(creators), created)
	})
}
