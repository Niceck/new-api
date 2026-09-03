package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequestBodyLimit_RejectsOversizedAuthenticatedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/bounded", RequestBodyLimit(16), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	tooLarge := httptest.NewRequest(http.MethodPost, "/bounded", strings.NewReader(strings.Repeat("x", 17)))
	tooLargeResponse := httptest.NewRecorder()
	router.ServeHTTP(tooLargeResponse, tooLarge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooLargeResponse.Code)

	allowed := httptest.NewRequest(http.MethodPost, "/bounded", strings.NewReader(strings.Repeat("x", 16)))
	allowedResponse := httptest.NewRecorder()
	router.ServeHTTP(allowedResponse, allowed)
	assert.Equal(t, http.StatusNoContent, allowedResponse.Code)
}
