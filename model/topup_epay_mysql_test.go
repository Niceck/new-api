package model

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 针对真实 MySQL 的易支付结算集成测试。
//
// 为什么必须单独跑 MySQL：model/locking.go 的 lockForUpdate 在 SQLite 下是
// 空操作（SQLite 无 FOR UPDATE 语法），且 sqlite 内存库测试把连接数限制为 1，
// 请求被连接池串行化。也就是说 sqlite 用例根本没有覆盖「行级锁真的生效」——
// 去掉 lockForUpdate 它们照样会过。并发双重入账只能在真实 MySQL 上验证。
//
// 用法（一次性可丢弃的库，不要指向生产）：
//   EPAY_TEST_MYSQL_DSN='user:pw@tcp(127.0.0.1:13306)/newapi_epay_test?charset=utf8mb4&parseTime=true&loc=Local' \
//     go test ./model/ -run TestRechargeEpayMySQL -v

func openEpayTestMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("EPAY_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("EPAY_TEST_MYSQL_DSN not configured")
	}
	// 防误伤：只允许打到一次性测试库。
	require.Contains(t, dsn, "newapi_epay_test", "must use the disposable test database")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// 并发用例需要多连接，绝不能设成 1，否则连接池会掩盖锁行为。
	sqlDB.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &Log{}))
	return db
}

// withEpayTestMySQL 把包级 DB 切到 MySQL 并声明数据库类型，
// 让 lockForUpdate 真正发出 SELECT ... FOR UPDATE。
func withEpayTestMySQL(t *testing.T, fn func()) {
	t.Helper()
	db := openEpayTestMySQL(t)

	prevDB, prevLogDB := DB, LOG_DB
	prevType := common.MainDatabaseType()
	prevRedis := common.RedisEnabled
	prevBatch := common.BatchUpdateEnabled

	DB, LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false

	t.Cleanup(func() {
		DB, LOG_DB = prevDB, prevLogDB
		common.SetMainDatabaseType(prevType)
		common.RedisEnabled = prevRedis
		common.BatchUpdateEnabled = prevBatch
	})

	fn()
}

// cleanEpayRows 清掉本用例的数据，允许反复运行（含 -count=N）。
//
// 必须用 Unscoped：User 带 DeletedAt，普通 Delete 是软删除，行仍物理存在，
// 下一轮以同一主键 Create 会撞 Error 1062 Duplicate entry。
func cleanEpayRows(t *testing.T, tradeNo string, userId int) {
	t.Helper()
	require.NoError(t, DB.Unscoped().Where("trade_no = ?", tradeNo).Delete(&TopUp{}).Error)
	require.NoError(t, DB.Unscoped().Where("id = ?", userId).Delete(&User{}).Error)
	require.NoError(t, DB.Unscoped().Where("user_id = ?", userId).Delete(&Log{}).Error)
}

// 真实 MySQL 下 lockForUpdate 必须让并发回调串行，只有一个入账。
// 这是 sqlite 用例无法覆盖的那一条。
func TestRechargeEpayMySQL_ConcurrentCallbacksCreditOnce(t *testing.T) {
	withEpayTestMySQL(t, func() {
		const tradeNo = "epay-mysql-concurrent"
		const userId = 930001
		cleanEpayRows(t, tradeNo, userId)
		t.Cleanup(func() { cleanEpayRows(t, tradeNo, userId) })

		topUp := epayFixture(t, tradeNo, userId, 10, 0)
		expected := int(decimalQuotaForAmount(10))

		const callers = 12
		var wg sync.WaitGroup
		var startGate sync.WaitGroup
		startGate.Add(1)

		credited := make([]int, callers)
		errs := make([]error, callers)
		wg.Add(callers)
		for i := 0; i < callers; i++ {
			go func(idx int) {
				defer wg.Done()
				startGate.Wait() // 尽量让所有 goroutine 同时冲进事务
				credited[idx], errs[idx] = RechargeEpay(topUp.TradeNo, "alipay", "10.00", "127.0.0.1")
			}(i)
		}
		startGate.Done()
		wg.Wait()

		total, creditedCallers := 0, 0
		for i := 0; i < callers; i++ {
			require.NoError(t, errs[i], "并发回调不得报错")
			total += credited[i]
			if credited[i] > 0 {
				creditedCallers++
			}
		}

		assert.Equal(t, 1, creditedCallers, "只能有一个调用方真正入账")
		assert.Equal(t, expected, total, "合计入账额度只能是一份")
		assert.Equal(t, expected, userQuota(t, userId), "用户额度只能增加一份")
		assert.Equal(t, common.TopUpStatusSuccess, topUpStatus(t, tradeNo))

		// 入账日志也只能有一条，否则说明事务边界没兜住。
		var topupLogs int64
		require.NoError(t, DB.Model(&Log{}).
			Where("user_id = ? AND type = ?", userId, LogTypeTopup).
			Count(&topupLogs).Error)
		assert.EqualValues(t, 1, topupLogs, "充值日志只能记一条")
	})
}

// 顺序重投在 MySQL 下同样幂等。
func TestRechargeEpayMySQL_IsIdempotentOnReplay(t *testing.T) {
	withEpayTestMySQL(t, func() {
		const tradeNo = "epay-mysql-replay"
		const userId = 930002
		cleanEpayRows(t, tradeNo, userId)
		t.Cleanup(func() { cleanEpayRows(t, tradeNo, userId) })

		topUp := epayFixture(t, tradeNo, userId, 10, 0)
		expected := int(decimalQuotaForAmount(10))

		first, err := RechargeEpay(topUp.TradeNo, "alipay", "10.00", "127.0.0.1")
		require.NoError(t, err)
		assert.Equal(t, expected, first)

		for i := 0; i < 3; i++ {
			again, err := RechargeEpay(topUp.TradeNo, "alipay", "10.00", "127.0.0.1")
			require.NoError(t, err, "重投必须幂等成功")
			assert.Equal(t, 0, again, "重投不得再次入账")
		}
		assert.Equal(t, expected, userQuota(t, userId))
	})
}

// 确认 lockForUpdate 在 MySQL 下确实发出了 FOR UPDATE（而非被静默跳过）。
// 这是上面并发用例成立的前提，单独钉住以防 locking.go 的分支被改动。
func TestRechargeEpayMySQL_EmitsSelectForUpdate(t *testing.T) {
	withEpayTestMySQL(t, func() {
		var captured []string
		session := DB.Session(&gorm.Session{DryRun: true})
		stmt := lockForUpdate(session).Model(&TopUp{}).
			Where("`trade_no` = ?", "epay-mysql-sql-shape").
			Find(&TopUp{}).Statement
		captured = append(captured, stmt.SQL.String())

		require.NotEmpty(t, captured)
		assert.True(t, strings.Contains(strings.ToUpper(captured[0]), "FOR UPDATE"),
			"MySQL 下必须发出 FOR UPDATE，实际 SQL：%s", captured[0])
	})
}

// 钱包上限在真实 MySQL 下同样整单回滚，不留部分入账。
func TestRechargeEpayMySQL_EnforcesQuotaUpperBound(t *testing.T) {
	withEpayTestMySQL(t, func() {
		const tradeNo = "epay-mysql-overflow"
		const userId = 930003
		cleanEpayRows(t, tradeNo, userId)
		t.Cleanup(func() { cleanEpayRows(t, tradeNo, userId) })

		epayFixture(t, tradeNo, userId, 10, common.MaxQuota-1)

		_, err := RechargeEpay(tradeNo, "alipay", "10.00", "127.0.0.1")
		require.Error(t, err)

		assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, tradeNo))
		assert.Equal(t, common.MaxQuota-1, userQuota(t, userId), "越界时不得部分入账")
	})
}
