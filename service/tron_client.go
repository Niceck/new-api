package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const (
	tronUSDTContract       = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	tronUSDTDecimals       = 6
	tronResponseMaxBytes   = 2 << 20
	tronMaxPaginationPages = 20
	tronPriceMaxAge        = 6 * time.Minute
	tronRequestTimeout     = 10 * time.Second
)

var tronTxIDPattern = regexp.MustCompile(`\A[0-9a-fA-F]{64}\z`)

type TronPriceClient interface {
	USDTToCNY(ctx context.Context) (rate decimal.Decimal, updatedAt time.Time, err error)
}

type TronChainClient interface {
	ConfirmedIncoming(ctx context.Context, fromMS, toMS int64) ([]TronTransfer, error)
}

type TronTransfer struct {
	TxID             string
	BlockTimestampMS int64
	From             string
	To               string
	AmountMicros     int64
}

type tronExternalAPIError struct {
	StatusCode int
	Retryable  bool
	Err        error
}

func (e *tronExternalAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("external API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("external API request failed: %v", e.Err)
}

func (e *tronExternalAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type coinGeckoPriceClient struct {
	httpClient     *http.Client
	baseURL        string
	apiKey         string
	now            func() time.Time
	requestTimeout time.Duration
}

func newCoinGeckoPriceClient(httpClient *http.Client, baseURL string, apiKey string, now func() time.Time) (*coinGeckoPriceClient, error) {
	if httpClient == nil || now == nil {
		return nil, errors.New("CoinGecko client dependencies are required")
	}
	if err := validateTronAPIBaseURL(baseURL, "api.coingecko.com"); err != nil {
		return nil, err
	}
	return &coinGeckoPriceClient{httpClient: cloneTronHTTPClient(httpClient), baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, now: now, requestTimeout: tronRequestTimeout}, nil
}

func (c *coinGeckoPriceClient) USDTToCNY(ctx context.Context) (decimal.Decimal, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	requestURL := c.baseURL + "/api/v3/simple/price?ids=tether&vs_currencies=cny&include_last_updated_at=true&precision=full"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-cg-demo-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return decimal.Zero, time.Time{}, err
		}
		return decimal.Zero, time.Time{}, &tronExternalAPIError{Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, time.Time{}, statusAPIError(resp.StatusCode)
	}

	var payload struct {
		Tether struct {
			CNY           json.Number `json:"cny"`
			LastUpdatedAt int64       `json:"last_updated_at"`
		} `json:"tether"`
	}
	if err := decodeTronJSON(resp.Body, &payload); err != nil {
		return decimal.Zero, time.Time{}, errors.New("invalid price response")
	}
	rate, err := decimal.NewFromString(payload.Tether.CNY.String())
	if err != nil || !rate.IsPositive() || rate.GreaterThan(decimal.NewFromInt(100)) {
		return decimal.Zero, time.Time{}, errors.New("invalid USDT/CNY price")
	}
	updatedAt := time.Unix(payload.Tether.LastUpdatedAt, 0)
	now := c.now()
	if payload.Tether.LastUpdatedAt <= 0 || updatedAt.After(now.Add(30*time.Second)) || now.Sub(updatedAt) > tronPriceMaxAge {
		return decimal.Zero, time.Time{}, errors.New("stale USDT/CNY price")
	}
	return rate, updatedAt, nil
}

type tronGridClient struct {
	httpClient     *http.Client
	baseURL        string
	receiveAddress string
	apiKey         string
	requestTimeout time.Duration
}

func newTronGridClient(httpClient *http.Client, baseURL string, receiveAddress string, apiKey string) (*tronGridClient, error) {
	if httpClient == nil {
		return nil, errors.New("TRON HTTP client is required")
	}
	if err := validateTronAPIBaseURL(baseURL, "api.trongrid.io"); err != nil {
		return nil, err
	}
	if err := validateTronAddress(receiveAddress); err != nil {
		return nil, err
	}
	return &tronGridClient{httpClient: cloneTronHTTPClient(httpClient), baseURL: strings.TrimRight(baseURL, "/"), receiveAddress: receiveAddress, apiKey: apiKey, requestTimeout: tronRequestTimeout}, nil
}

func (c *tronGridClient) ConfirmedIncoming(ctx context.Context, fromMS, toMS int64) ([]TronTransfer, error) {
	if fromMS < 0 || toMS <= fromMS {
		return nil, errors.New("invalid TRON scan window")
	}

	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	grouped := make(map[string]*TronTransfer)
	order := make([]string, 0)
	fingerprint := ""
	seenFingerprints := make(map[string]struct{})
	for page := 0; page < tronMaxPaginationPages; page++ {
		payload, err := c.fetchTransferPage(ctx, fromMS, toMS, fingerprint)
		if err != nil {
			return nil, err
		}
		for _, row := range payload.Data {
			transfer, err := c.parseTransfer(row)
			if err != nil {
				return nil, err
			}
			if transfer.BlockTimestampMS < fromMS || transfer.BlockTimestampMS > toMS {
				return nil, errors.New("TronGrid transfer is outside requested window")
			}
			existing := grouped[transfer.TxID]
			if existing == nil {
				copyOfTransfer := transfer
				grouped[transfer.TxID] = &copyOfTransfer
				order = append(order, transfer.TxID)
				continue
			}
			if existing.BlockTimestampMS != transfer.BlockTimestampMS || existing.From != transfer.From || existing.To != transfer.To {
				return nil, errors.New("inconsistent transfer rows for transaction")
			}
			if transfer.AmountMicros > math.MaxInt64-existing.AmountMicros {
				return nil, errors.New("TRON transfer amount overflow")
			}
			existing.AmountMicros += transfer.AmountMicros
		}

		nextFingerprint := payload.Meta.Fingerprint
		if nextFingerprint == "" {
			transfers := make([]TronTransfer, 0, len(order))
			for _, txID := range order {
				transfers = append(transfers, *grouped[txID])
			}
			return transfers, nil
		}
		if nextFingerprint == fingerprint {
			return nil, errors.New("TronGrid pagination did not advance")
		}
		if _, exists := seenFingerprints[nextFingerprint]; exists {
			return nil, errors.New("TronGrid pagination loop detected")
		}
		seenFingerprints[nextFingerprint] = struct{}{}
		fingerprint = nextFingerprint
	}
	return nil, errors.New("TronGrid pagination limit exceeded")
}

type tronGridResponse struct {
	Success bool                 `json:"success"`
	Data    []tronGridRow        `json:"data"`
	Meta    tronGridResponseMeta `json:"meta"`
}

type tronGridResponseMeta struct {
	Fingerprint string `json:"fingerprint"`
}

type tronGridRow struct {
	TransactionID string            `json:"transaction_id"`
	BlockTimeMS   int64             `json:"block_timestamp"`
	From          string            `json:"from"`
	To            string            `json:"to"`
	Type          string            `json:"type"`
	Value         string            `json:"value"`
	TokenInfo     tronGridTokenInfo `json:"token_info"`
}

type tronGridTokenInfo struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

func (c *tronGridClient) fetchTransferPage(ctx context.Context, fromMS, toMS int64, fingerprint string) (tronGridResponse, error) {
	params := url.Values{}
	params.Set("only_confirmed", "true")
	params.Set("only_to", "true")
	params.Set("contract_address", tronUSDTContract)
	params.Set("limit", "200")
	params.Set("order_by", "block_timestamp,asc")
	params.Set("min_timestamp", strconv.FormatInt(fromMS, 10))
	params.Set("max_timestamp", strconv.FormatInt(toMS, 10))
	if fingerprint != "" {
		params.Set("fingerprint", fingerprint)
	}
	requestURL := c.baseURL + "/v1/accounts/" + url.PathEscape(c.receiveAddress) + "/transactions/trc20?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return tronGridResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return tronGridResponse{}, err
		}
		return tronGridResponse{}, &tronExternalAPIError{Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return tronGridResponse{}, statusAPIError(resp.StatusCode)
	}
	var payload tronGridResponse
	if err := decodeTronJSON(resp.Body, &payload); err != nil {
		return tronGridResponse{}, errors.New("invalid TronGrid response")
	}
	if !payload.Success {
		return tronGridResponse{}, errors.New("TronGrid response was unsuccessful")
	}
	return payload, nil
}

func (c *tronGridClient) parseTransfer(row tronGridRow) (TronTransfer, error) {
	if !tronTxIDPattern.MatchString(row.TransactionID) {
		return TronTransfer{}, errors.New("invalid TRON transaction id")
	}
	if row.BlockTimeMS <= 0 || row.To != c.receiveAddress || row.Type != "Transfer" {
		return TronTransfer{}, errors.New("invalid TRON transfer identity")
	}
	if row.TokenInfo.Address != tronUSDTContract || row.TokenInfo.Symbol != "USDT" || row.TokenInfo.Decimals != tronUSDTDecimals {
		return TronTransfer{}, errors.New("invalid TRON token identity")
	}
	amount, err := strconv.ParseInt(row.Value, 10, 64)
	if err != nil || amount <= 0 {
		return TronTransfer{}, errors.New("invalid TRON transfer amount")
	}
	return TronTransfer{TxID: strings.ToLower(row.TransactionID), BlockTimestampMS: row.BlockTimeMS, From: row.From, To: row.To, AmountMicros: amount}, nil
}

func statusAPIError(status int) error {
	retryable := status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
	return &tronExternalAPIError{StatusCode: status, Retryable: retryable}
}

func validateTronAPIBaseURL(rawURL string, officialHost string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return errors.New("invalid external API base URL")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "https" && hostname == officialHost && parsed.Port() == "" {
		return nil
	}
	if parsed.Scheme == "http" && (hostname == "127.0.0.1" || hostname == "::1") {
		return nil
	}
	return errors.New("external API base URL is not an approved origin")
}

func cloneTronHTTPClient(source *http.Client) *http.Client {
	client := *source
	if client.Timeout == 0 || client.Timeout > tronRequestTimeout {
		client.Timeout = tronRequestTimeout
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func decodeTronJSON(reader io.Reader, target any) error {
	body, err := io.ReadAll(io.LimitReader(reader, tronResponseMaxBytes+1))
	if err != nil {
		return err
	}
	if len(body) > tronResponseMaxBytes {
		return errors.New("external API response is too large")
	}
	return common.Unmarshal(body, target)
}
