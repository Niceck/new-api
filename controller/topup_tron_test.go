package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTronTopupOperations struct {
	createView      service.TronOrderView
	createUserID    int
	createAmount    int64
	createPayCNY    decimal.Decimal
	createQuota     int
	createCalls     int
	getView         service.TronOrderView
	getUserID       int
	getTradeNo      string
	claimTicket     *model.TronTopupTicket
	claimUserID     int
	claimTradeNo    string
	claimTxID       string
	resolveTicketID int64
	resolveOrderID  int64
	resolveResolver int
	resolveNote     string
	rejectTicketID  int64
}

func (s *stubTronTopupOperations) CreateOrder(_ context.Context, userID int, amount int64, payCNY decimal.Decimal, quota int) (service.TronOrderView, error) {
	s.createCalls++
	s.createUserID = userID
	s.createAmount = amount
	s.createPayCNY = payCNY
	s.createQuota = quota
	return s.createView, nil
}

func (s *stubTronTopupOperations) GetOrder(userID int, tradeNo string) (service.TronOrderView, error) {
	s.getUserID = userID
	s.getTradeNo = tradeNo
	return s.getView, nil
}

func (s *stubTronTopupOperations) SubmitClaim(userID int, tradeNo string, txID string, note string) (*model.TronTopupTicket, error) {
	s.claimUserID = userID
	s.claimTradeNo = tradeNo
	s.claimTxID = txID
	return s.claimTicket, nil
}

func (s *stubTronTopupOperations) ResolveTicket(_ context.Context, ticketID int64, orderID int64, resolverID int, note string) error {
	s.resolveTicketID = ticketID
	s.resolveOrderID = orderID
	s.resolveResolver = resolverID
	s.resolveNote = note
	return nil
}

func (s *stubTronTopupOperations) RejectTicket(ticketID int64, resolverID int, note string) error {
	s.rejectTicketID = ticketID
	return nil
}

func withTronControllerDependencies(t *testing.T, operations *stubTronTopupOperations) {
	t.Helper()
	originalOperations := getTronTopupOperations
	originalGroup := getTronUserGroup
	originalMoney := calculateTronPaymentMoney
	originalPublicConfig := getTronPublicConfig
	originalCompliance := isTopupComplianceConfirmed
	getTronTopupOperations = func() (tronTopupOperations, error) { return operations, nil }
	getTronUserGroup = func(_ int, _ bool) (string, error) { return "default", nil }
	calculateTronPaymentMoney = func(_ int64, _ string) float64 { return 100 }
	getTronPublicConfig = func() (service.TronTopupConfig, bool) {
		return service.TronTopupConfig{Enabled: true, Network: "mainnet", ReceiveAddress: "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f"}, true
	}
	isTopupComplianceConfirmed = func() bool { return true }
	t.Cleanup(func() {
		getTronTopupOperations = originalOperations
		getTronUserGroup = originalGroup
		calculateTronPaymentMoney = originalMoney
		getTronPublicConfig = originalPublicConfig
		isTopupComplianceConfirmed = originalCompliance
	})
}

func (s *stubTronTopupOperations) ListTickets(_ *common.PageInfo, _ string) ([]*model.TronTopupTicket, int64, error) {
	return nil, 0, nil
}

func (s *stubTronTopupOperations) AdminStatus() (service.TronTopupAdminStatus, error) {
	return service.TronTopupAdminStatus{Enabled: true}, nil
}

func tronHandlerResponse(t *testing.T, method string, path string, body string, userID int, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return tronHandlerResponseAt(t, method, path, path, body, userID, handler)
}

func tronHandlerResponseAt(t *testing.T, method string, routePath string, requestPath string, body string, userID int, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(c *gin.Context) {
		c.Set("id", userID)
		handler(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, requestPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestCreateTronTopupOrder_UsesServerCalculatedMoneyAndQuota(t *testing.T) {
	operations := &stubTronTopupOperations{createView: service.TronOrderView{TradeNo: "TRON-order", Status: common.TopUpStatusPending}}
	withTronControllerDependencies(t, operations)
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Equal(t, 42, operations.createUserID)
	assert.Equal(t, int64(100), operations.createAmount)
	assert.True(t, operations.createPayCNY.Equal(decimal.NewFromInt(100)))
	assert.Equal(t, 50_000_000, operations.createQuota)

	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":0}`, 42, CreateTronTopupOrder)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Equal(t, 1, operations.createCalls)
}

func TestCreateTronTopupOrder_RejectsDirectCallWhenComplianceIsDisabled(t *testing.T) {
	operations := &stubTronTopupOperations{createView: service.TronOrderView{TradeNo: "must-not-create"}}
	withTronControllerDependencies(t, operations)
	isTopupComplianceConfirmed = func() bool { return false }

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Zero(t, operations.createCalls)
}

func TestGetTronTopupOrder_PassesAuthenticatedUserScope(t *testing.T) {
	operations := &stubTronTopupOperations{getView: service.TronOrderView{TradeNo: "TRON-owned"}}
	withTronControllerDependencies(t, operations)

	recorder := tronHandlerResponseAt(t, http.MethodGet, "/:trade_no", "/TRON-owned", "", 51, GetTronTopupOrder)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 51, operations.getUserID)
	assert.Equal(t, "TRON-owned", operations.getTradeNo)

	operations.getTradeNo = ""
	recorder = tronHandlerResponseAt(t, http.MethodGet, "/:trade_no", "/"+strings.Repeat("x", 256), "", 51, GetTronTopupOrder)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Empty(t, operations.getTradeNo)
}

func TestSubmitTronTopupClaim_RejectsInvalidTxIDBeforeService(t *testing.T) {
	operations := &stubTronTopupOperations{claimTicket: &model.TronTopupTicket{ID: 1}}
	withTronControllerDependencies(t, operations)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"trade_no":"TRON-owned","tx_id":"not-a-tx","note":"help"}`, 52, SubmitTronTopupClaim)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Empty(t, operations.claimTxID)

	txID := strings.Repeat("A", 64)
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"trade_no":"TRON-owned","tx_id":"`+txID+`","note":"exchange fee"}`, 52, SubmitTronTopupClaim)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Equal(t, strings.ToLower(txID), operations.claimTxID)

	operations.claimTxID = ""
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"trade_no":"`+strings.Repeat("x", 256)+`","tx_id":"`+txID+`","note":"too long"}`, 52, SubmitTronTopupClaim)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Empty(t, operations.claimTxID)
}

func TestAdminResolveTronTopupTicket_RequiresSelectedOrderAndNote(t *testing.T) {
	operations := &stubTronTopupOperations{}
	withTronControllerDependencies(t, operations)

	recorder := tronHandlerResponseAt(t, http.MethodPost, "/:id", "/7", `{"order_id":0,"note":""}`, 1, AdminResolveTronTopupTicket)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Zero(t, operations.resolveTicketID)

	recorder = tronHandlerResponseAt(t, http.MethodPost, "/:id", "/7", `{"order_id":99,"note":"verified ownership"}`, 1, AdminResolveTronTopupTicket)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Equal(t, int64(7), operations.resolveTicketID)
	assert.Equal(t, int64(99), operations.resolveOrderID)
	assert.Equal(t, 1, operations.resolveResolver)
	assert.Equal(t, "verified ownership", operations.resolveNote)
}

func TestGetTopUpInfo_AppendsTronOnlyWhenConfigured(t *testing.T) {
	operations := &stubTronTopupOperations{}
	withTronControllerDependencies(t, operations)

	recorder := tronHandlerResponse(t, http.MethodGet, "/", "", 1, GetTopUpInfo)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"type":"tron"`)
	assert.NotContains(t, recorder.Body.String(), "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f")
}
