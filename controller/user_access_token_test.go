package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAccessTokenRejectsPATWithoutMutation(t *testing.T) {
	db := setupManageUserTestDB(t)
	oldToken := "existing-personal-access-token"
	user := model.User{
		Username: "pat-rotation-rejected",
		Role:     common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AuthVersion: 1, AccessToken: &oldToken,
	}
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/token", nil)
	c.Set("id", user.Id)
	c.Set("use_access_token", true)

	GenerateAccessToken(c)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "dashboard session authentication")
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, oldToken, stored.GetAccessToken())
}

func TestGenerateAccessTokenWithSessionRotatesCurrentUserToken(t *testing.T) {
	db := setupManageUserTestDB(t)
	oldToken := "existing-session-user-token"
	user := model.User{
		Username: "session-rotation-allowed",
		Role:     common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AuthVersion: 1, AccessToken: &oldToken,
	}
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/token", nil)
	c.Set("id", user.Id)
	c.Set("session_id", "test-session")
	c.Set("auth_version", int64(1))
	c.Set("session_version", int64(1))

	GenerateAccessToken(c)

	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.NotEmpty(t, payload.Data)
	assert.NotEqual(t, oldToken, payload.Data)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, payload.Data, stored.GetAccessToken())
}
