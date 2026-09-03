package controller

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type tronTopupOperations interface {
	CreateOrder(ctx context.Context, userID int, amount int64, payCNY decimal.Decimal, quota int) (service.TronOrderView, error)
	GetOrder(userID int, tradeNo string) (service.TronOrderView, error)
	SubmitClaim(userID int, tradeNo string, txID string, note string) (*model.TronTopupTicket, error)
	ListTickets(pageInfo *common.PageInfo, status string) ([]*model.TronTopupTicket, int64, error)
	ResolveTicket(ctx context.Context, ticketID int64, orderID int64, resolverID int, note string) error
	RejectTicket(ticketID int64, resolverID int, note string) error
	AdminStatus() (service.TronTopupAdminStatus, error)
}

var (
	tronControllerTxIDPattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
	getTronTopupOperations    = defaultTronTopupOperations
	getTronUserGroup          = model.GetUserGroup
	calculateTronPaymentMoney = getPayMoney
	getTronPublicConfig       = service.GetTronTopupPublicConfig
)

func defaultTronTopupOperations() (tronTopupOperations, error) {
	return service.GetDefaultTronTopupService()
}

type createTronTopupRequest struct {
	Amount int64 `json:"amount"`
}

func CreateTronTopupOrder(c *gin.Context) {
	if !isTopupComplianceConfirmed() {
		common.ApiErrorMsg(c, "支付合规条款尚未确认")
		return
	}
	var request createTronTopupRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Amount < getMinTopup() {
		common.ApiErrorMsg(c, "充值金额无效")
		return
	}
	userID := c.GetInt("id")
	group, err := getTronUserGroup(userID, true)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}
	payCNY := decimal.NewFromFloat(calculateTronPaymentMoney(request.Amount, group))
	if !payCNY.IsPositive() {
		common.ApiErrorMsg(c, "充值金额无效")
		return
	}

	accountingAmount := request.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		accountingAmount = decimal.NewFromInt(request.Amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	quotaDecimal := decimal.NewFromInt(accountingAmount).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	creditQuota, clamp := common.QuotaFromDecimalChecked(quotaDecimal)
	if clamp != nil || creditQuota <= 0 {
		common.ApiErrorMsg(c, "充值金额超过安全上限")
		return
	}
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	order, err := operations.CreateOrder(c.Request.Context(), userID, accountingAmount, payCNY, creditQuota)
	if err != nil {
		logger.LogWarn(c.Request.Context(), "create TRON top-up order failed: "+err.Error())
		common.ApiErrorMsg(c, "TRON 充值订单创建失败，请稍后重试")
		return
	}
	common.ApiSuccess(c, order)
}

func GetTronTopupOrder(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	if tradeNo == "" || len(tradeNo) > 255 {
		common.ApiErrorMsg(c, "充值订单号格式错误")
		return
	}
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	order, err := operations.GetOrder(c.GetInt("id"), tradeNo)
	if err != nil {
		if errors.Is(err, model.ErrTronOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "充值订单不存在"})
			return
		}
		common.ApiErrorMsg(c, "查询 TRON 充值订单失败")
		return
	}
	common.ApiSuccess(c, order)
}

type submitTronClaimRequest struct {
	TradeNo string `json:"trade_no"`
	TxID    string `json:"tx_id"`
	Note    string `json:"note"`
}

func SubmitTronTopupClaim(c *gin.Context) {
	var request submitTronClaimRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	request.TradeNo = strings.TrimSpace(request.TradeNo)
	request.TxID = strings.ToLower(strings.TrimSpace(request.TxID))
	request.Note = strings.TrimSpace(request.Note)
	if request.TradeNo == "" || len(request.TradeNo) > 255 || !tronControllerTxIDPattern.MatchString(request.TxID) || len([]rune(request.Note)) > 500 {
		common.ApiErrorMsg(c, "订单号、txid 或备注格式错误")
		return
	}
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	ticket, err := operations.SubmitClaim(c.GetInt("id"), request.TradeNo, request.TxID, request.Note)
	if err != nil {
		common.ApiErrorMsg(c, "充值申诉提交失败")
		return
	}
	common.ApiSuccess(c, ticket)
}

func AdminListTronTopupTickets(c *gin.Context) {
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	pageInfo := common.GetPageQuery(c)
	tickets, total, err := operations.ListTickets(pageInfo, strings.TrimSpace(c.Query("status")))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tickets)
	common.ApiSuccess(c, pageInfo)
}

type resolveTronTicketRequest struct {
	OrderID int64  `json:"order_id"`
	Note    string `json:"note"`
}

func AdminResolveTronTopupTicket(c *gin.Context) {
	ticketID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request resolveTronTicketRequest
	if err != nil || ticketID <= 0 || c.ShouldBindJSON(&request) != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	request.Note = strings.TrimSpace(request.Note)
	if request.OrderID <= 0 || request.Note == "" || len([]rune(request.Note)) > 500 {
		common.ApiErrorMsg(c, "必须选择核验后的订单并填写处理说明")
		return
	}
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	if err := operations.ResolveTicket(c.Request.Context(), ticketID, request.OrderID, c.GetInt("id"), request.Note); err != nil {
		logger.LogWarn(c.Request.Context(), "resolve TRON top-up ticket failed: "+err.Error())
		common.ApiErrorMsg(c, "工单处理失败：链上核验未通过或交易已处理")
		return
	}
	common.ApiSuccess(c, nil)
}

type rejectTronTicketRequest struct {
	Note string `json:"note"`
}

func AdminRejectTronTopupTicket(c *gin.Context) {
	ticketID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request rejectTronTicketRequest
	if err != nil || ticketID <= 0 || c.ShouldBindJSON(&request) != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	request.Note = strings.TrimSpace(request.Note)
	if request.Note == "" || len([]rune(request.Note)) > 500 {
		common.ApiErrorMsg(c, "必须填写拒绝原因")
		return
	}
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	if err := operations.RejectTicket(ticketID, c.GetInt("id"), request.Note); err != nil {
		common.ApiErrorMsg(c, "工单拒绝失败")
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminGetTronTopupStatus(c *gin.Context) {
	operations, err := getTronTopupOperations()
	if err != nil {
		common.ApiErrorMsg(c, "TRON 充值暂不可用")
		return
	}
	status, err := operations.AdminStatus()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}
