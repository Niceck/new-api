package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTronRouteAuthTest(t *testing.T) string {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousRedis := common.RedisEnabled
	previousSQLitePath := common.SQLitePath
	previousMaster := common.IsMasterNode
	common.SQLitePath = filepath.Join(t.TempDir(), "tron-route-test.db")
	common.IsMasterNode = true
	common.RedisEnabled = false
	t.Setenv("SQL_DSN", "")
	require.NoError(t, model.InitDB())
	db := model.DB
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.RedisEnabled = previousRedis
		common.SQLitePath = previousSQLitePath
		common.IsMasterNode = previousMaster
	})
	token := "tron-route-user-token"
	user := &model.User{Username: "tron-route-user", Password: "password-placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1, AffCode: "tron-route-aff"}
	require.NoError(t, db.Create(user).Error)
	return token
}

func TestTronTopupRoutes_EnforceUserAndAdminAuthenticationAndBodyLimit(t *testing.T) {
	token := setupTronRouteAuthTest(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	unauthenticated := httptest.NewRequest(http.MethodPost, "/api/user/tron/topup/orders", strings.NewReader(`{"amount":100}`))
	unauthenticated.Header.Set("Content-Type", "application/json")
	unauthenticatedResponse := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticatedResponse, unauthenticated)
	assert.Equal(t, http.StatusUnauthorized, unauthenticatedResponse.Code)

	nonAdmin := httptest.NewRequest(http.MethodGet, "/api/user/tron/topup/status", nil)
	nonAdmin.Header.Set("Authorization", "Bearer "+token)
	nonAdminResponse := httptest.NewRecorder()
	engine.ServeHTTP(nonAdminResponse, nonAdmin)
	assert.Equal(t, http.StatusForbidden, nonAdminResponse.Code)

	oversized := httptest.NewRequest(http.MethodPost, "/api/user/tron/topup/orders", strings.NewReader(`{"amount":100,"padding":"`+strings.Repeat("x", 4096)+`"}`))
	oversized.Header.Set("Authorization", "Bearer "+token)
	oversized.Header.Set("Content-Type", "application/json")
	oversizedResponse := httptest.NewRecorder()
	engine.ServeHTTP(oversizedResponse, oversized)
	assert.Equal(t, http.StatusRequestEntityTooLarge, oversizedResponse.Code)
}
