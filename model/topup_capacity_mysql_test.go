package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 钱包容量检查在真实 MySQL 上的并发用例。
//
// SQLite 用例（topup_capacity_test.go）只能证明算术对：lockForUpdate 在 SQLite 下是空操作，
// 连接数又被限成 1。「同一用户同时下两张单，后到的看得见先到的」靠的是用户行上的
// SELECT ... FOR UPDATE，只有真实 MySQL 能验证——去掉那把锁，下面的用例必须变红。
//
// 用法与 topup_epay_mysql_test.go 相同（一次性可丢弃的库，不要指向生产）：
//   EPAY_TEST_MYSQL_DSN='...@tcp(127.0.0.1:13306)/newapi_epay_test?charset=utf8mb4&parseTime=true&loc=Local' \
//     go test ./model/ -run TestTopUpCapacityMySQL -v

func cleanCapacityRows(t *testing.T, userID int) {
	t.Helper()
	require.NoError(t, DB.Unscoped().Where("user_id = ?", userID).Delete(&TronTopupOrder{}).Error)
	require.NoError(t, DB.Unscoped().Where("user_id = ?", userID).Delete(&TopUp{}).Error)
	require.NoError(t, DB.Unscoped().Where("id = ?", userID).Delete(&User{}).Error)
}

func pendingOrderCount(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("user_id = ? AND status = ?", userID, common.TopUpStatusPending).Count(&count).Error)
	return count
}

// 余额 522，同时冲进来 12 张 2000 的易支付订单：只能建成一张（522 + 2000 装得下，再多一张就超限）。
func TestTopUpCapacityMySQL_ConcurrentEpayOrdersReserveOnce(t *testing.T) {
	withEpayTestMySQL(t, func() {
		require.NoError(t, DB.AutoMigrate(&TronTopupOrder{}))
		const userID = 930101
		cleanCapacityRows(t, userID)
		t.Cleanup(func() { cleanCapacityRows(t, userID) })
		capacityUser(t, userID, yuan(522))

		const callers = 12
		errs := make([]error, callers)
		var wg sync.WaitGroup
		var startGate sync.WaitGroup
		startGate.Add(1)
		wg.Add(callers)
		for i := 0; i < callers; i++ {
			go func(idx int) {
				defer wg.Done()
				startGate.Wait()
				errs[idx] = InsertTopUpWithinWalletCapacity(
					epayOrder(userID, fmt.Sprintf("cap-mysql-epay-%d", idx), 2000, capacityNow))
			}(i)
		}
		startGate.Done()
		wg.Wait()

		created, refused := 0, 0
		for i := 0; i < callers; i++ {
			switch {
			case errs[i] == nil:
				created++
			case errors.Is(errs[i], ErrTopUpWalletFull):
				refused++
			default:
				t.Fatalf("并发下单只应成功或被容量拒绝，实际：%v", errs[i])
			}
		}
		assert.Equal(t, 1, created, "只能建成一张")
		assert.Equal(t, callers-1, refused)
		assert.Equal(t, int64(1), pendingOrderCount(t, userID))
	})
}

// 易支付与 TRON 同时下单：两条路径锁的是同一行用户，合计也只能建成一张。
func TestTopUpCapacityMySQL_ConcurrentEpayAndTronShareTheSameRoom(t *testing.T) {
	withEpayTestMySQL(t, func() {
		require.NoError(t, DB.AutoMigrate(&TronTopupOrder{}))
		const userID = 930102
		cleanCapacityRows(t, userID)
		t.Cleanup(func() { cleanCapacityRows(t, userID) })
		capacityUser(t, userID, yuan(522))

		const pairs = 6
		errs := make([]error, pairs*2)
		var wg sync.WaitGroup
		var startGate sync.WaitGroup
		startGate.Add(1)
		wg.Add(pairs * 2)
		for i := 0; i < pairs; i++ {
			go func(idx int) {
				defer wg.Done()
				startGate.Wait()
				errs[idx*2] = InsertTopUpWithinWalletCapacity(
					epayOrder(userID, fmt.Sprintf("cap-mysql-mix-epay-%d", idx), 2000, capacityNow))
			}(i)
			go func(idx int) {
				defer wg.Done()
				topUp, order, nowMS := tronCreatePair(userID, fmt.Sprintf("cap-mysql-mix-tron-%d", idx), yuan(2000))
				order.ExpectedAmountMicros += int64(idx) // 应付金额唯一
				startGate.Wait()
				// 已有一张有效 TRON 订单时会原样返回它（created=false、err=nil），不算新建
				_, created, err := CreateOrGetActiveTronTopupOrder(topUp, order, nowMS)
				if err == nil && !created {
					err = errReusedActiveTronOrder
				}
				errs[idx*2+1] = err
			}(i)
		}
		startGate.Done()
		wg.Wait()

		created := 0
		for _, err := range errs {
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrTopUpWalletFull), errors.Is(err, errReusedActiveTronOrder):
			default:
				t.Fatalf("并发下单只应成功、被容量拒绝或复用已有订单，实际：%v", err)
			}
		}
		assert.Equal(t, 1, created, "两条渠道合计只能建成一张")
		assert.Equal(t, int64(1), pendingOrderCount(t, userID))
	})
}

var errReusedActiveTronOrder = errors.New("reused active TRON order")

// 管理员补单与回调入账同时发生：两边都锁用户行，余额不会被补过上限。
func TestTopUpCapacityMySQL_ManualCompleteRespectsLimitUnderConcurrency(t *testing.T) {
	withEpayTestMySQL(t, func() {
		const userID = 930103
		cleanCapacityRows(t, userID)
		t.Cleanup(func() { cleanCapacityRows(t, userID) })
		capacityUser(t, userID, yuan(522))
		const orders = 4
		for i := 0; i < orders; i++ {
			seedOrder(t, epayOrder(userID, fmt.Sprintf("cap-mysql-manual-%d", i), 2000, capacityNow), common.TopUpStatusPending)
		}

		errs := make([]error, orders)
		var wg sync.WaitGroup
		var startGate sync.WaitGroup
		startGate.Add(1)
		wg.Add(orders)
		for i := 0; i < orders; i++ {
			go func(idx int) {
				defer wg.Done()
				startGate.Wait()
				errs[idx] = ManualCompleteTopUp(fmt.Sprintf("cap-mysql-manual-%d", idx), "127.0.0.1")
			}(i)
		}
		startGate.Done()
		wg.Wait()

		completed := 0
		for _, err := range errs {
			if err == nil {
				completed++
			}
		}
		assert.Equal(t, 1, completed, "522 + 2000 之后再补任何一张都超限")
		assert.Equal(t, yuan(2522), userQuota(t, userID))
	})
}
