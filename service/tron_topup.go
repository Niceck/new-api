package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

const (
	defaultTronTopupMaxCNY         int64 = 2_000
	maximumTronTopupMaxCNY         int64 = 2_000
	defaultTronOrderTTLMinutes           = 20
	defaultTronScanIntervalSeconds       = 30
)

type TronTopupConfig struct {
	Enabled           bool
	Network           string
	ReceiveAddress    string
	TronGridBaseURL   string
	CoinGeckoBaseURL  string
	TronGridAPIKey    string
	CoinGeckoAPIKey   string
	OrderTTL          time.Duration
	ScanInterval      time.Duration
	InitialLookback   time.Duration
	CheckpointOverlap time.Duration
	IndexSafetyLag    time.Duration
	ReconcileLookback time.Duration
	ReconcileInterval time.Duration
	MaxCNY            int64
}

type TronOrderView struct {
	TradeNo            string `json:"trade_no"`
	Network            string `json:"network"`
	ReceiveAddress     string `json:"receive_address"`
	TokenContract      string `json:"token_contract"`
	ExpectedUSDTMicros int64  `json:"expected_usdt_micros"`
	RateCNYMicros      int64  `json:"rate_cny_micros"`
	QuoteUpdatedAtMS   int64  `json:"quote_updated_at_ms"`
	ExpiresAtMS        int64  `json:"expires_at_ms"`
	CreditQuota        int    `json:"credit_quota"`
	Status             string `json:"status"`
}

type TronTopupAdminStatus struct {
	Enabled             bool   `json:"enabled"`
	Network             string `json:"network"`
	ReceiveAddress      string `json:"receive_address"`
	CheckpointMS        int64  `json:"checkpoint_ms"`
	CheckpointUpdatedMS int64  `json:"checkpoint_updated_ms"`
	OpenTickets         int64  `json:"open_tickets"`
}

type tronTopupService struct {
	config          TronTopupConfig
	priceClient     TronPriceClient
	chainClient     TronChainClient
	now             func() time.Time
	probeSeedSource func() (int64, error)
	reconcileMu     sync.Mutex
	lastReconcileMS int64
}

var (
	defaultTronTopupServiceMu           sync.RWMutex
	defaultTronService                  *tronTopupService
	ErrTronChainVerificationUnavailable = errors.New("TRON chain verification is unavailable")
)

func loadTronTopupConfig() (TronTopupConfig, error) {
	config := TronTopupConfig{Enabled: common.GetEnvOrDefaultBool("TRON_TOPUP_ENABLED", false)}
	if !config.Enabled {
		return config, nil
	}
	config.Network = strings.ToLower(strings.TrimSpace(common.GetEnvOrDefaultString("TRON_NETWORK", "mainnet")))
	if config.Network != "mainnet" {
		return TronTopupConfig{}, errors.New("TRON top-up only supports mainnet")
	}
	config.ReceiveAddress = strings.TrimSpace(os.Getenv("TRON_RECEIVE_ADDRESS"))
	if err := validateTronAddress(config.ReceiveAddress); err != nil {
		return TronTopupConfig{}, fmt.Errorf("invalid TRON_RECEIVE_ADDRESS: %w", err)
	}
	config.TronGridBaseURL = "https://api.trongrid.io"
	config.CoinGeckoBaseURL = "https://api.coingecko.com"
	config.TronGridAPIKey = strings.TrimSpace(os.Getenv("TRONGRID_API_KEY"))
	config.CoinGeckoAPIKey = strings.TrimSpace(os.Getenv("COINGECKO_API_KEY"))
	if config.TronGridAPIKey == "" || config.CoinGeckoAPIKey == "" {
		return TronTopupConfig{}, errors.New("TRON top-up read API keys are required")
	}

	ttlMinutes, err := strictTronEnvInt("TRON_TOPUP_ORDER_TTL_MINUTES", defaultTronOrderTTLMinutes, 5, 60)
	if err != nil {
		return TronTopupConfig{}, err
	}
	intervalSeconds, err := strictTronEnvInt("TRON_TOPUP_SCAN_INTERVAL_SECONDS", defaultTronScanIntervalSeconds, 15, 300)
	if err != nil {
		return TronTopupConfig{}, err
	}
	maxCNY, err := strictTronEnvInt("TRON_TOPUP_MAX_CNY", int(defaultTronTopupMaxCNY), 1, int(maximumTronTopupMaxCNY))
	if err != nil {
		return TronTopupConfig{}, err
	}
	config.OrderTTL = time.Duration(ttlMinutes) * time.Minute
	config.ScanInterval = time.Duration(intervalSeconds) * time.Second
	config.IndexSafetyLag = 3 * time.Minute
	config.CheckpointOverlap = 2 * time.Minute
	config.InitialLookback = config.OrderTTL + config.IndexSafetyLag + config.CheckpointOverlap
	config.ReconcileLookback = 25 * time.Hour
	config.ReconcileInterval = time.Hour
	config.MaxCNY = int64(maxCNY)
	return config, nil
}

func strictTronEnvInt(name string, defaultValue int, minimum int, maximum int) (int, error) {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func newTronTopupService(config TronTopupConfig, priceClient TronPriceClient, chainClient TronChainClient, now func() time.Time, probeSeedSource func() (int64, error)) *tronTopupService {
	return &tronTopupService{config: config, priceClient: priceClient, chainClient: chainClient, now: now, probeSeedSource: probeSeedSource}
}

func InitTronTopupService() error {
	config, err := loadTronTopupConfig()
	if err != nil {
		return err
	}
	defaultTronTopupServiceMu.Lock()
	defer defaultTronTopupServiceMu.Unlock()
	if !config.Enabled {
		config.Network = "mainnet"
		defaultTronService = newTronTopupService(config, nil, nil, time.Now, nil)
		return nil
	}
	baseClient := GetHttpClient()
	if baseClient == nil {
		return errors.New("HTTP client is not initialized for TRON top-up")
	}
	client := &http.Client{Transport: baseClient.Transport, CheckRedirect: baseClient.CheckRedirect, Timeout: tronRequestTimeout}
	chainClient, err := newTronGridClient(client, config.TronGridBaseURL, config.ReceiveAddress, config.TronGridAPIKey)
	if err != nil {
		return err
	}
	priceClient, err := newCoinGeckoPriceClient(client, config.CoinGeckoBaseURL, config.CoinGeckoAPIKey, time.Now)
	if err != nil {
		return err
	}
	defaultTronService = newTronTopupService(config, priceClient, chainClient, time.Now, randomTronProbeSeed)
	return nil
}

func TronTopupEnabled() bool {
	defaultTronTopupServiceMu.RLock()
	defer defaultTronTopupServiceMu.RUnlock()
	return defaultTronService != nil && defaultTronService.config.Enabled
}

func GetTronTopupPublicConfig() (TronTopupConfig, bool) {
	defaultTronTopupServiceMu.RLock()
	defer defaultTronTopupServiceMu.RUnlock()
	if defaultTronService == nil || !defaultTronService.config.Enabled {
		return TronTopupConfig{}, false
	}
	config := defaultTronService.config
	config.TronGridAPIKey = ""
	config.CoinGeckoAPIKey = ""
	return config, true
}

func GetDefaultTronTopupService() (*tronTopupService, error) {
	defaultTronTopupServiceMu.RLock()
	defer defaultTronTopupServiceMu.RUnlock()
	if defaultTronService == nil {
		return nil, errors.New("TRON top-up is not enabled")
	}
	return defaultTronService, nil
}

func (s *tronTopupService) CreateOrder(ctx context.Context, userID int, requestedAmount int64, payCNY decimal.Decimal, creditQuota int) (TronOrderView, error) {
	if !s.config.Enabled || s.priceClient == nil || s.now == nil || s.probeSeedSource == nil || userID <= 0 || requestedAmount <= 0 || !payCNY.IsPositive() || payCNY.GreaterThan(decimal.NewFromInt(s.config.MaxCNY)) {
		return TronOrderView{}, errors.New("TRON top-up amount is out of range")
	}
	if creditQuota <= 0 || creditQuota > common.MaxQuota {
		return TronOrderView{}, errors.New("TRON top-up quota is out of range")
	}
	now := s.now()
	nowMS := now.UnixMilli()
	active, err := model.GetActiveTronTopupOrderForUser(userID, nowMS)
	if err != nil {
		return TronOrderView{}, err
	}
	if active != nil {
		return tronOrderView(active, common.TopUpStatusPending, s.config.Network), nil
	}

	rate, quoteUpdatedAt, err := s.priceClient.USDTToCNY(ctx)
	if err != nil {
		return TronOrderView{}, fmt.Errorf("USDT/CNY price unavailable: %w", err)
	}
	if quoteUpdatedAt.After(now.Add(30*time.Second)) || now.Sub(quoteUpdatedAt) > tronPriceMaxAge {
		return TronOrderView{}, errors.New("USDT/CNY price is stale")
	}
	probeSeed, err := s.probeSeedSource()
	if err != nil {
		return TronOrderView{}, errors.New("failed to allocate TRON payment amount")
	}
	if probeSeed < tronProbeSeedMin || probeSeed > tronProbeSeedMax {
		return TronOrderView{}, errors.New("TRON payment probe seed source returned invalid value")
	}

	for attempt := int64(0); attempt < tronUniqueCandidateCount; attempt++ {
		offsetMicros := tronPaymentUniqueOffset(attempt, probeSeed)
		quote, err := calculateTronPayment(payCNY, rate, creditQuota, offsetMicros)
		if err != nil {
			return TronOrderView{}, err
		}
		exists, err := model.TronExpectedAmountExists(quote.ExpectedUSDTMicros)
		if err != nil {
			return TronOrderView{}, err
		}
		if exists {
			continue
		}
		tradeSuffix, err := common.GenerateRandomCharsKey(16)
		if err != nil {
			return TronOrderView{}, errors.New("failed to generate TRON order number")
		}
		tradeNo := "TRONUSR" + strconv.Itoa(userID) + "NO" + tradeSuffix
		payMoney, _ := payCNY.Float64()
		topUp := &model.TopUp{UserId: userID, Amount: requestedAmount, Money: payMoney, TradeNo: tradeNo, PaymentMethod: model.PaymentMethodTron, PaymentProvider: model.PaymentProviderTron, CreateTime: now.Unix(), Status: common.TopUpStatusPending}
		order := &model.TronTopupOrder{UserID: userID, TradeNo: tradeNo, ReceiveAddress: s.config.ReceiveAddress, TokenContract: tronUSDTContract, ExpectedAmountMicros: quote.ExpectedUSDTMicros, RateCNYMicros: quote.RateCNYMicros, QuoteUpdatedAtMS: quoteUpdatedAt.UnixMilli(), ExpiresAtMS: now.Add(s.config.OrderTTL).UnixMilli(), CreditQuota: quote.CreditQuota, CreatedAtMS: nowMS}
		storedOrder, created, err := model.CreateOrGetActiveTronTopupOrder(topUp, order, nowMS)
		if err != nil {
			collision, collisionErr := model.TronExpectedAmountExists(quote.ExpectedUSDTMicros)
			if collisionErr == nil && collision {
				continue
			}
			return TronOrderView{}, err
		}
		if !created {
			return tronOrderView(storedOrder, common.TopUpStatusPending, s.config.Network), nil
		}
		return tronOrderView(storedOrder, topUp.Status, s.config.Network), nil
	}
	return TronOrderView{}, errors.New("no unique TRON payment amount is available")
}

func tronOrderView(order *model.TronTopupOrder, status string, network string) TronOrderView {
	return TronOrderView{TradeNo: order.TradeNo, Network: network, ReceiveAddress: order.ReceiveAddress, TokenContract: order.TokenContract, ExpectedUSDTMicros: order.ExpectedAmountMicros, RateCNYMicros: order.RateCNYMicros, QuoteUpdatedAtMS: order.QuoteUpdatedAtMS, ExpiresAtMS: order.ExpiresAtMS, CreditQuota: order.CreditQuota, Status: status}
}

func tronPaymentUniqueOffset(attempt int64, probeSeed int64) int64 {
	if attempt == 0 {
		return 0
	}
	magnitude := (attempt + 1) / 2
	positive := attempt%2 == 1
	if probeSeed%2 == 1 {
		positive = !positive
	}
	if positive {
		return magnitude
	}
	return -magnitude
}

func randomTronProbeSeed() (int64, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(tronUniqueCandidateCount))
	if err != nil {
		return 0, err
	}
	return value.Int64() + 1, nil
}

func (s *tronTopupService) GetOrder(userID int, tradeNo string) (TronOrderView, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if userID <= 0 || tradeNo == "" || len(tradeNo) > 255 {
		return TronOrderView{}, errors.New("invalid TRON top-up order query")
	}
	order, topUp, err := model.GetTronTopupOrderForUser(userID, tradeNo)
	if err != nil {
		return TronOrderView{}, err
	}
	status := topUp.Status
	if status == common.TopUpStatusPending && s.now().UnixMilli() > order.ExpiresAtMS {
		status = common.TopUpStatusExpired
	}
	return tronOrderView(order, status, s.config.Network), nil
}

func (s *tronTopupService) SubmitClaim(userID int, tradeNo string, txID string, note string) (*model.TronTopupTicket, error) {
	return model.SubmitTronTopupClaim(userID, tradeNo, txID, note)
}

func (s *tronTopupService) ListTickets(pageInfo *common.PageInfo, status string) ([]*model.TronTopupTicket, int64, error) {
	return model.ListTronTopupTickets(pageInfo, status)
}

func (s *tronTopupService) ResolveTicket(ctx context.Context, ticketID int64, orderID int64, resolverID int, note string) error {
	if s.chainClient == nil {
		return ErrTronChainVerificationUnavailable
	}
	ticket, err := model.GetTronTopupTicketByID(ticketID)
	if err != nil {
		return err
	}
	order, err := model.GetTronTopupOrderByID(orderID)
	if err != nil {
		return err
	}
	now := s.now()
	upperMS := now.Add(-s.config.IndexSafetyLag).UnixMilli()
	claimUpperMS := order.ExpiresAtMS + model.TronClaimGracePeriodMS
	if claimUpperMS < upperMS {
		upperMS = claimUpperMS
	}
	if upperMS <= order.CreatedAtMS {
		return errors.New("TRON transaction is not yet available for confirmed review")
	}
	transfers, err := s.chainClient.ConfirmedIncoming(ctx, order.CreatedAtMS, upperMS)
	if err != nil {
		return err
	}
	for _, transfer := range transfers {
		if transfer.TxID != ticket.TxID {
			continue
		}
		record := model.TronTransferRecord{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.From, ToAddress: transfer.To, TokenContract: tronUSDTContract, AmountMicros: transfer.AmountMicros, ObservedAtMS: now.UnixMilli()}
		return model.ResolveTronTopupTicket(ticketID, orderID, record, resolverID, note)
	}
	return errors.New("confirmed TRON transaction not found")
}

func (s *tronTopupService) RejectTicket(ticketID int64, resolverID int, note string) error {
	return model.RejectTronTopupTicket(ticketID, resolverID, note, s.now().UnixMilli())
}

func (s *tronTopupService) AdminStatus() (TronTopupAdminStatus, error) {
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	if err != nil {
		return TronTopupAdminStatus{}, err
	}
	openTickets, err := model.CountOpenTronTopupTickets()
	if err != nil {
		return TronTopupAdminStatus{}, err
	}
	status := TronTopupAdminStatus{Enabled: s.config.Enabled, Network: s.config.Network, ReceiveAddress: s.config.ReceiveAddress, OpenTickets: openTickets}
	if checkpoint != nil {
		status.CheckpointMS = checkpoint.LastScannedMS
		status.CheckpointUpdatedMS = checkpoint.UpdatedAtMS
	}
	return status, nil
}
