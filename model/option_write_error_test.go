package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 配置写库失败要报错，内存也不能变：否则接口显示新值、60 秒后的定时重载再冲回去，配置就静默丢了。
func TestUpdateOption_DatabaseFailureIsReportedAndMemoryStaysOld(t *testing.T) {
	const key = "TestUpdateOptionWriteFailure"
	require.NoError(t, DB.AutoMigrate(&Option{}))
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, UpdateOption(key, "old"))

	boom := errors.New("database is gone")
	const name = "test:fail_option_save"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			_ = tx.AddError(boom)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(name) })

	assert.ErrorIs(t, UpdateOption(key, "new"), boom)
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	assert.Equal(t, "old", common.OptionMap[key])
}
