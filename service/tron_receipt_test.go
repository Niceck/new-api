package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTronReceipt_IndexDuplicatesDoNotInflatePayment(t *testing.T) {
	txid := strings.Repeat("a", 64)
	address := "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f"
	decoded, err := decodeTronBase58(address)
	require.NoError(t, err)
	contract, err := decodeTronBase58(tronUSDTContract)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/walletsolidity/gettransactioninfobyid" {
			_, _ = fmt.Fprintf(w, `{"id":"%s","blockTimeStamp":1500,"receipt":{"result":"SUCCESS"},"log":[{"address":"%x","topics":["ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef","%064x","%064x"],"data":"%064x"}]}`, txid, contract[1:21], 1, decoded[1:21], 1000000)
			return
		}
		page := tronGridPageJSON("", txid, "1000000")
		row := page[strings.Index(page, "[{")+1 : strings.Index(page, "}],")+1]
		_, _ = fmt.Fprintf(w, `{"success":true,"data":[%s,%s],"meta":{}}`, row, row)
	}))
	defer server.Close()
	client, err := newTronGridClient(server.Client(), server.URL, address, "")
	require.NoError(t, err)
	transfers, err := client.ConfirmedIncoming(context.Background(), 1000, 2000)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	assert.Equal(t, int64(1000000), transfers[0].AmountMicros)
}

func TestTronReceipt_ExecutionContractDestinationAndSources(t *testing.T) {
	address := "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f"
	destination, err := decodeTronBase58(address)
	require.NoError(t, err)
	contract, err := decodeTronBase58(tronUSDTContract)
	require.NoError(t, err)
	txid := strings.Repeat("b", 64)
	event := func(sender, amount int, wrongContract, wrongDestination bool) string {
		token := fmt.Sprintf("%x", contract[1:21])
		to := fmt.Sprintf("%064x", destination[1:21])
		if wrongContract {
			token = strings.Repeat("1", 40)
		}
		if wrongDestination {
			to = fmt.Sprintf("%064x", 42)
		}
		return fmt.Sprintf(`{"address":"%s","topics":["ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef","%064x","%s"],"data":"%064x"}`, token, sender, to, amount)
	}
	for _, tc := range []struct {
		name, result, logs string
		want               int64
		multi              bool
	}{
		{"zero ignored", "SUCCESS", event(1, 0, false, false), 0, false},
		{"failed execution", "REVERT", event(1, 100, false, false), 0, false},
		{"fake token", "SUCCESS", event(1, 100, true, false), 0, false},
		{"wrong destination", "SUCCESS", event(1, 100, false, true), 0, false},
		{"same sender distinct events", "SUCCESS", event(1, 100, false, false) + "," + event(1, 100, false, false), 200, false},
		{"different senders", "SUCCESS", event(1, 100, false, false) + "," + event(2, 100, false, false), 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/walletsolidity/gettransactioninfobyid" {
					_, _ = fmt.Fprintf(w, `{"id":"%s","blockTimeStamp":1500,"receipt":{"result":"%s"},"log":[%s]}`, txid, tc.result, tc.logs)
					return
				}
				_, _ = w.Write([]byte(tronGridPageJSON("", txid, "100")))
			}))
			defer server.Close()
			client, err := newTronGridClient(server.Client(), server.URL, address, "")
			require.NoError(t, err)
			transfers, err := client.ConfirmedIncoming(context.Background(), 1000, 2000)
			require.NoError(t, err)
			if tc.want == 0 {
				assert.Empty(t, transfers)
				return
			}
			require.Len(t, transfers, 1)
			assert.Equal(t, tc.want, transfers[0].AmountMicros)
			assert.Equal(t, tc.multi, transfers[0].SourceAddresses != "")
		})
	}
}
