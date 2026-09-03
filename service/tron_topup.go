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
	tronOrderTailAttempts                = 9_999
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

type tronTopupService struct {
	config          TronTopupConfig
	priceClient     TronPriceClient
	chainClient     TronChainClient
	now             func() time.Time
	tailSource      func() (int64, error)
	reconcileMu     sync.Mutex
	lastReconcileMS int64
}

var (
	defaultTronTopupServiceMu sync.RWMutex
	defaultTronService        *tronTopupService
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

func newTronTopupService(config TronTopupConfig, priceClient TronPriceClient, chainClient TronChainClient, now func() time.Time, tailSource func() (int64, error)) *tronTopupService {
	return &tronTopupService{config: config, priceClient: priceClient, chainClient: chainClient, now: now, tailSource: tailSource}
}

func InitTronTopupService() error {
	config, err := loadTronTopupConfig()
	if err != nil {
		return err
	}
	defaultTronTopupServiceMu.Lock()
	defer defaultTronTopupServiceMu.Unlock()
	if !config.Enabled {
		defaultTronService = nil
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
	defaultTronService = newTronTopupService(config, priceClient, chainClient, time.Now, randomTronTail)
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
	if defaultTronService == nil || !defaultTronService.config.Enabled {
		return nil, errors.New("TRON top-up is not enabled")
	}
	return defaultTronService, nil
}

func (s *tronTopupService) CreateOrder(ctx context.Context, userID int, requestedAmount int64, payCNY decimal.Decimal, creditQuota int) (TronOrderView, error) {
	if !s.config.Enabled || s.priceClient == nil || s.now == nil || s.tailSource == nil || userID <= 0 || requestedAmount <= 0 || !payCNY.IsPositive() || payCNY.GreaterThan(decimal.NewFromInt(s.config.MaxCNY)) {
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
	startTail, err := s.tailSource()
	if err != nil {
		return TronOrderView{}, errors.New("failed to allocate TRON payment amount")
	}
	if startTail < tronMinTail || startTail > tronMaxTail {
		return TronOrderView{}, errors.New("TRON payment tail source returned invalid value")
	}

	for attempt := int64(0); attempt < tronOrderTailAttempts; attempt++ {
		tail := ((startTail - 1 + attempt) % tronMaxTail) + 1
		quote, err := calculateTronPayment(payCNY, rate, creditQuota, tail)
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

func randomTronTail() (int64, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(tronMaxTail))
	if err != nil {
		return 0, err
	}
	return value.Int64() + 1, nil
}
