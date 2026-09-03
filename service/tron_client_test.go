package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoinGeckoPriceClient_AcceptsFreshPositivePrice(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/simple/price", r.URL.Path)
		assert.Equal(t, "tether", r.URL.Query().Get("ids"))
		assert.Equal(t, "cny", r.URL.Query().Get("vs_currencies"))
		assert.Equal(t, "true", r.URL.Query().Get("include_last_updated_at"))
		assert.Equal(t, "demo-key", r.Header.Get("x-cg-demo-api-key"))
		_, _ = fmt.Fprintf(w, `{"tether":{"cny":7.251234,"last_updated_at":%d}}`, now.Unix()-20)
	}))
	defer server.Close()

	client, err := newCoinGeckoPriceClient(server.Client(), server.URL, "demo-key", func() time.Time { return now })
	require.NoError(t, err)
	rate, updatedAt, err := client.USDTToCNY(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "7.251234", rate.String())
	assert.Equal(t, now.Unix()-20, updatedAt.Unix())
}

func TestCoinGeckoPriceClient_RejectsStaleMalformedAndNonPositivePrices(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	testCases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "stale", status: http.StatusOK, body: fmt.Sprintf(`{"tether":{"cny":7.2,"last_updated_at":%d}}`, now.Unix()-121)},
		{name: "zero", status: http.StatusOK, body: fmt.Sprintf(`{"tether":{"cny":0,"last_updated_at":%d}}`, now.Unix())},
		{name: "missing", status: http.StatusOK, body: `{}`},
		{name: "malformed", status: http.StatusOK, body: `{`},
		{name: "server error", status: http.StatusBadGateway, body: `{}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client, err := newCoinGeckoPriceClient(server.Client(), server.URL, "", func() time.Time { return now })
			require.NoError(t, err)
			_, _, err = client.USDTToCNY(context.Background())
			assert.Error(t, err)
		})
	}
}

func TestTronGridClient_UsesFixedFiltersPaginatesAndGroupsByTxID(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/v1/accounts/TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f/transactions/trc20", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("only_confirmed"))
		assert.Equal(t, "true", r.URL.Query().Get("only_to"))
		assert.Equal(t, tronUSDTContract, r.URL.Query().Get("contract_address"))
		assert.Equal(t, "200", r.URL.Query().Get("limit"))
		assert.Equal(t, "block_timestamp,asc", r.URL.Query().Get("order_by"))
		assert.Equal(t, "1000", r.URL.Query().Get("min_timestamp"))
		assert.Equal(t, "2000", r.URL.Query().Get("max_timestamp"))
		assert.Equal(t, "tron-key", r.Header.Get("TRON-PRO-API-KEY"))

		fingerprint := r.URL.Query().Get("fingerprint")
		if fingerprint == "" {
			_, _ = w.Write([]byte(tronGridPageJSON("fp-next", strings.Repeat("a", 64), "1000000")))
			return
		}
		assert.Equal(t, "fp-next", fingerprint)
		_, _ = w.Write([]byte(tronGridPageJSON("", strings.Repeat("a", 64), "2000000")))
	}))
	defer server.Close()

	client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "tron-key")
	require.NoError(t, err)
	transfers, err := client.ConfirmedIncoming(context.Background(), 1000, 2000)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	assert.Equal(t, int64(3_000_000), transfers[0].AmountMicros)
	assert.Equal(t, strings.Repeat("a", 64), transfers[0].TxID)
	assert.Equal(t, int64(1500), transfers[0].BlockTimestampMS)
	assert.Equal(t, int32(2), requests.Load())
}

func TestTronGridClient_RejectsMalformedRowsAndReportsRetryableStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("min_timestamp") == "1" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[{"transaction_id":"` + strings.Repeat("a", 64) + `","block_timestamp":1500,"from":"TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh","to":"wrong","type":"Transfer","value":"1","token_info":{"symbol":"USDT","address":"` + tronUSDTContract + `","decimals":6}}],"meta":{}}`))
	}))
	defer server.Close()

	client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
	require.NoError(t, err)
	_, err = client.ConfirmedIncoming(context.Background(), 2, 3)
	assert.Error(t, err)

	_, err = client.ConfirmedIncoming(context.Background(), 1, 3)
	var apiErr *tronExternalAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.True(t, apiErr.Retryable)
}

func TestTronGridClient_HonorsCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ConfirmedIncoming(ctx, 1, 2)
	assert.ErrorIs(t, err, context.Canceled)
}

func tronGridPageJSON(fingerprint, txID, value string) string {
	meta := `{}`
	if fingerprint != "" {
		meta = `{"fingerprint":"` + url.QueryEscape(fingerprint) + `"}`
	}
	return `{"success":true,"data":[{"transaction_id":"` + txID + `","block_timestamp":1500,"from":"TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh","to":"TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f","type":"Transfer","value":"` + value + `","token_info":{"symbol":"USDT","address":"` + tronUSDTContract + `","decimals":6}}],"meta":` + meta + `}`
}

func TestTronExternalAPIError_UnwrapsContextErrors(t *testing.T) {
	err := &tronExternalAPIError{Err: context.DeadlineExceeded, Retryable: true}
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestExternalClients_RejectUnsafeConstructionAndRedirects(t *testing.T) {
	_, err := newCoinGeckoPriceClient(nil, "https://api.coingecko.com", "key", time.Now)
	assert.Error(t, err)
	_, err = newCoinGeckoPriceClient(http.DefaultClient, "http://example.com", "key", time.Now)
	assert.Error(t, err)
	_, err = newCoinGeckoPriceClient(http.DefaultClient, "https://user@example.com?leak=1", "key", time.Now)
	assert.Error(t, err)
	_, err = newTronGridClient(http.DefaultClient, "http://example.com", "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "key")
	assert.Error(t, err)

	redirectTargetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirectTargetCalled = true
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	client, err := newCoinGeckoPriceClient(redirect.Client(), redirect.URL, "secret-key", time.Now)
	require.NoError(t, err)
	_, _, err = client.USDTToCNY(context.Background())
	assert.Error(t, err)
	assert.False(t, redirectTargetCalled)
}

func TestExternalClients_EnforceRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := newCoinGeckoPriceClient(server.Client(), server.URL, "", time.Now)
	require.NoError(t, err)
	client.requestTimeout = 20 * time.Millisecond

	_, _, err = client.USDTToCNY(context.Background())
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestExternalClients_RejectOversizedAndTrailingJSON(t *testing.T) {
	for _, body := range []string{
		`{"tether":{"cny":7.2,"last_updated_at":1800000000}} trailing`,
		strings.Repeat(" ", tronResponseMaxBytes+1),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		client, err := newCoinGeckoPriceClient(server.Client(), server.URL, "", func() time.Time { return time.Unix(1_800_000_000, 0) })
		require.NoError(t, err)
		_, _, err = client.USDTToCNY(context.Background())
		assert.Error(t, err)
		server.Close()
	}
}

func TestCoinGeckoPriceClient_RejectsFutureAndOversizedPrice(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, body := range []string{
		fmt.Sprintf(`{"tether":{"cny":7.2,"last_updated_at":%d}}`, now.Unix()+31),
		fmt.Sprintf(`{"tether":{"cny":101,"last_updated_at":%d}}`, now.Unix()),
		fmt.Sprintf(`{"tether":{"cny":-1,"last_updated_at":%d}}`, now.Unix()),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		client, err := newCoinGeckoPriceClient(server.Client(), server.URL, "", func() time.Time { return now })
		require.NoError(t, err)
		_, _, err = client.USDTToCNY(context.Background())
		assert.Error(t, err)
		server.Close()
	}
}

func TestTronGridClient_RejectsTokenAndTransferContractViolations(t *testing.T) {
	validTx := strings.Repeat("a", 64)
	baseRow := `{"transaction_id":"%s","block_timestamp":1500,"from":"TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh","to":"TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f","type":"%s","value":"%s","token_info":{"symbol":"%s","address":"%s","decimals":%d}}`
	testCases := []string{
		fmt.Sprintf(baseRow, validTx, "Approval", "1", "USDT", tronUSDTContract, 6),
		fmt.Sprintf(baseRow, validTx, "Transfer", "1.5", "USDT", tronUSDTContract, 6),
		fmt.Sprintf(baseRow, validTx, "Transfer", "1", "FAKE", tronUSDTContract, 6),
		fmt.Sprintf(baseRow, validTx, "Transfer", "1", "USDT", "wrong", 6),
		fmt.Sprintf(baseRow, validTx, "Transfer", "1", "USDT", tronUSDTContract, 18),
		fmt.Sprintf(baseRow, validTx, "Transfer", "1", "USDT", tronUSDTContract, 6),
	}

	for index, row := range testCases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if index == len(testCases)-1 {
				row = strings.Replace(row, `"block_timestamp":1500`, `"block_timestamp":999`, 1)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":[` + row + `],"meta":{}}`))
		}))
		client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
		require.NoError(t, err)
		_, err = client.ConfirmedIncoming(context.Background(), 1000, 2000)
		assert.Error(t, err, "case %d", index)
		server.Close()
	}
}

func TestTronGridClient_RejectsPaginationLoopsCapsAndAggregationOverflow(t *testing.T) {
	validTx := strings.Repeat("a", 64)
	t.Run("loop", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tronGridPageJSON("same", validTx, "1")))
		}))
		defer server.Close()
		client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
		require.NoError(t, err)
		_, err = client.ConfirmedIncoming(context.Background(), 1000, 2000)
		assert.Error(t, err)
	})

	t.Run("page cap", func(t *testing.T) {
		counter := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			counter++
			_, _ = w.Write([]byte(tronGridPageJSON(fmt.Sprintf("fp-%d", counter), validTx, "1")))
		}))
		defer server.Close()
		client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
		require.NoError(t, err)
		_, err = client.ConfirmedIncoming(context.Background(), 1000, 2000)
		assert.Error(t, err)
		assert.Equal(t, tronMaxPaginationPages, counter)
	})

	t.Run("amount overflow", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("fingerprint") == "" {
				_, _ = w.Write([]byte(tronGridPageJSON("next", validTx, "9223372036854775807")))
				return
			}
			_, _ = w.Write([]byte(tronGridPageJSON("", validTx, "1")))
		}))
		defer server.Close()
		client, err := newTronGridClient(server.Client(), server.URL, "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", "")
		require.NoError(t, err)
		_, err = client.ConfirmedIncoming(context.Background(), 1000, 2000)
		assert.Error(t, err)
	})
}

func TestStatusAPIError_ClassifiesTimeoutRateLimitAndAllServerErrorsRetryable(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		var apiErr *tronExternalAPIError
		require.ErrorAs(t, statusAPIError(status), &apiErr)
		assert.True(t, apiErr.Retryable, status)
	}
	var apiErr *tronExternalAPIError
	require.ErrorAs(t, statusAPIError(http.StatusBadRequest), &apiErr)
	assert.False(t, apiErr.Retryable)
}
