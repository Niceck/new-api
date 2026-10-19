package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 配置框架用 strconv.ParseBool 解析、解析失败静默跳过：写进库里的怪值重启后才发现没生效。
// 这两个开关直接决定扣多少钱、什么时候扣，写入时只认 true / false。
func TestUpdateOptionRejectsNonCanonicalQuotaSwitches(t *testing.T) {
	for _, key := range []string{"quota_setting.trust_bypass_enabled", "quota_setting.round_charge_to_cny_cent"} {
		for _, value := range []string{`"yes"`, `"1"`, `"True"`, `""`, `" false"`} {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodPut, "/api/option/",
				strings.NewReader(`{"key":"`+key+`","value":`+value+`}`))

			UpdateOption(context)

			assert.Equal(t, http.StatusOK, response.Code)
			var payload struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.False(t, payload.Success, "%s=%s", key, value)
			assert.Contains(t, payload.Message, key)
		}
	}
}
