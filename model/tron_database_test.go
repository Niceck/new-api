package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTronIsolatedDatabaseCompatibility(t *testing.T) {
	for _, engine := range []string{"MYSQL", "POSTGRES"} {
		t.Run(engine, func(t *testing.T) {
			dsn := os.Getenv("TRON_TEST_" + engine + "_DSN")
			if dsn == "" {
				t.Skip("isolated database not configured")
			}
			require.Contains(t, dsn, "newapi_tron_test", "must use the disposable test database")
			var dialector gorm.Dialector
			if engine == "MYSQL" {
				dialector = mysql.Open(dsn)
			} else {
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			previousType := common.MainDatabaseType()
			if engine == "MYSQL" {
				common.SetMainDatabaseType(common.DatabaseTypeMySQL)
			} else {
				common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
			}
			defer common.SetMainDatabaseType(previousType)
			previous := DB
			DB = db
			defer func() { DB = previous }()
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TronTopupOrder{}, &TronDeposit{}, &TronTopupTicket{}, &TronScanCheckpoint{}, &TronSerialLock{}))
			require.NoError(t, EnsureTronSerialLock())
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TronTopupOrder{}, &TronDeposit{}, &TronTopupTicket{}, &TronScanCheckpoint{}, &TronSerialLock{}))
			require.NoError(t, EnsureTronSerialLock())
			_, order := createTronLedgerFixture(t, "TRON-external-"+strings.ToLower(engine), 8910, 1_000_234, 1_800_001_200_000)
			first := tronTransfer("d", order.ExpectedAmountMicros, order.CreatedAtMS+1000)
			_, err = SettleTronDeposit(first, "test")
			require.NoError(t, err)
			second := tronTransfer("e", order.ExpectedAmountMicros, order.CreatedAtMS+2000)
			result, err := SettleTronDeposit(second, "test")
			require.NoError(t, err)
			assert.True(t, result.NeedsReview)
			replay, err := SettleTronDeposit(first, "test")
			require.NoError(t, err)
			assert.True(t, replay.AlreadyProcessed)
			summary, err := GetTronDepositSummary()
			require.NoError(t, err)
			assert.Equal(t, int64(1), summary.ReviewCount)
			wide, err := BeginTronReconciliation("test", 1000, 2000, 3000, 1000)
			require.NoError(t, err)
			require.NotNil(t, wide)
			require.NoError(t, AdvanceTronReconciliation("test", 1500, 3100))
			wide, err = BeginTronReconciliation("test", 4000, 5000, 6000, 1000)
			require.NoError(t, err)
			assert.Equal(t, int64(2000), wide.WindowEndMS)
			assert.Equal(t, int64(1500), wide.LastScannedMS)

			// 容差路径在这个方言上也要走通：串行锁的写入、金额范围查询、少付按比例入账、
			// 未匹配落库与重扫幂等。
			tolerantOrder := tolOrder(t, "TRON-external-tol-"+strings.ToLower(engine), 8911, 21_000_000, tolBaseMS, tolFaceQuota)
			underpaid := tolTransfer(0x8911, 20_000_000, tolBaseMS+113_000)
			settled, err := SettleTronDepositTolerant(underpaid, nil, "test")
			require.NoError(t, err)
			assert.True(t, settled.Credited)
			assert.Equal(t, tolerantOrder.ID, settled.OrderID)
			assert.Equal(t, 47_619_047, tolQuota(t, 8911))
			again, err := SettleTronDepositTolerant(underpaid, nil, "test")
			require.NoError(t, err)
			assert.True(t, again.AlreadyProcessed)
			assert.Equal(t, 47_619_047, tolQuota(t, 8911))
			stray, err := SettleTronDepositTolerant(tolTransfer(0x8912, 99_000_000, tolBaseMS+113_000), nil, "test")
			require.NoError(t, err)
			assert.True(t, stray.Unmatched)
			assert.Equal(t, tronTolerantNoteNoCandidate, stray.MatchNote)
		})
	}
}
