# TRON / TRC20-USDT Top-up Implementation Plan

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add production-grade TRON mainnet TRC20-USDT top-ups with locked USDT/CNY pricing, confirmed-chain scanning, idempotent crediting, and two auditable support-ticket fallbacks.

**Architecture:** Keep generic top-up records intact and add TRON-specific order, deposit, ticket, and checkpoint tables. A master-only background scanner reads confirmed inbound official USD₮ transfers from TronGrid and calls a single transactional settlement path shared by automatic and administrator-reviewed recovery. React adds a dedicated QR/payment-status dialog and claim flow while operations code configures and monitors the feature.

**Tech Stack:** Go 1.26, Gin, GORM (SQLite/MySQL/PostgreSQL compatible), shopspring/decimal, React 19/TypeScript, Bun, Base UI/Tailwind, qrcode.react, Docker Compose, Python pytest operations tooling.

---

### Task 1: Fixed-point pricing, configuration, and external clients

**Files:**
- Create: `service/tron_client.go`
- Create: `service/tron_client_test.go`
- Create: `service/tron_money.go`
- Create: `service/tron_money_test.go`
- Create: `service/tron_address.go`
- Create: `service/tron_address_test.go`

**Step 1: Write failing money tests**

Add deterministic table tests for:

```go
func TestCalculateTronPayment_UsesFixedPointAndBoundsTail(t *testing.T)
func TestCalculateTronPayment_RejectsZeroNegativeAndQuotaOverflow(t *testing.T)
func TestCalculateClaimQuota_IsProportionalAndNeverOverflows(t *testing.T)
```

The wished-for API is:

```go
type TronPaymentQuote struct {
    ExpectedUSDTMicros int64
    RateCNYMicros      int64
    CreditQuota        int
}

func calculateTronPayment(payCNY decimal.Decimal, rateCNY decimal.Decimal, creditQuota int, tail int64) (TronPaymentQuote, error)
func calculateClaimQuota(expectedMicros int64, actualMicros int64, creditQuota int) (int, error)
```

**Step 2: Verify RED**

Run:

```bash
docker run --rm -v "$PWD:/work" -w /work golang:1.26.1-alpine \
  go test ./service -run 'TestCalculateTronPayment|TestCalculateClaimQuota' -count=1
```

Expected: FAIL because the types/functions do not exist.

**Step 3: Implement minimal fixed-point math**

Use `decimal.Decimal` until the final integer conversion. Store USDT in 10^-6 units and CNY/USDT in 10^-6 units. Tail must be in `[1, 9999]`, the resulting payment must be positive, and `creditQuota` must be within `common.MaxQuota`. Claim quota is proportional to actual/expected payment, rounded down and rejected on zero/overflow.

**Step 4: Write failing HTTP client tests**

Use `httptest.Server` only at the external boundary. Cover:

- CoinGecko valid price and `last_updated_at`.
- stale, missing, NaN/negative, oversized, malformed and non-200 prices.
- TronGrid fixed filters, API-key header, ascending pagination and fingerprint preservation.
- `value` integer parsing, official contract/decimals/type/address checks.
- same-tx rows are grouped and summed with overflow protection.
- 429/503 returns a typed retryable error; context cancellation stops requests.
- Base58Check accepts the configured TRON address and rejects bad alphabet, length, network byte, and checksum.

Define narrow clients:

```go
type TronPriceClient interface {
    USDTToCNY(ctx context.Context) (rate decimal.Decimal, updatedAt time.Time, err error)
}

type TronChainClient interface {
    ConfirmedIncoming(ctx context.Context, fromMS, toMS int64) ([]TronTransfer, error)
}
```

**Step 5: Verify RED, implement clients, then verify GREEN**

Keep URLs in a validated config object; production network values map to fixed official base URLs, while tests inject an `httptest` URL. Send `TRON-PRO-API-KEY` and CoinGecko key only when configured. Bound body reads, set HTTP timeouts, use `common.DecodeJson`, validate addresses with Base58Check, and never log keys or raw response bodies.

Run the Task 1 tests, then `go test ./service -count=1`. Expected: PASS.

**Step 6: Commit**

```bash
git add service/tron_client.go service/tron_client_test.go service/tron_money.go service/tron_money_test.go service/tron_address.go service/tron_address_test.go
git commit -m "feat: add TRON pricing and chain clients"
```

### Task 2: TRON order, deposit, checkpoint, ticket, and idempotent settlement models

**Files:**
- Create: `model/tron_topup.go`
- Create: `model/tron_topup_test.go`
- Modify: `model/topup.go`
- Modify: `model/main.go`

**Step 1: Write failing cross-model contract tests**

Extend the package’s SQLite TestMain fixture and add tests for:

```go
func TestCreateTronTopupOrder_CreatesGenericAndTronRowsAtomically(t *testing.T)
func TestCreateTronTopupOrder_ExpectedAmountIsGloballyUnique(t *testing.T)
func TestSettleTronDeposit_CreditsExactlyOnceOnReplay(t *testing.T)
func TestSettleTronDeposit_RejectsWrongProviderAmountContractAddressAndLateBlock(t *testing.T)
func TestSettleTronDeposit_RollsBackOrderDepositAndQuotaTogether(t *testing.T)
func TestSubmitTronClaim_PreventsCrossUserTxidClaim(t *testing.T)
func TestResolveTronTicket_UsesVerifiedDepositAndCannotReuseTxid(t *testing.T)
```

Use `testify/require` and `testify/assert`. Assert observable rows and quota, not helper internals.

**Step 2: Verify RED**

Run `go test ./model -run 'Tron' -count=1`. Expected: FAIL because models are absent.

**Step 3: Add cross-database models and migrations**

Add:

```go
type TronTopupOrder struct { /* TopUpID unique, UserID, address, contract, fixed-point amounts/rate, quote/expires timestamps, CreditQuota */ }
type TronDeposit struct { /* TxID unique, timestamps, addresses, contract, AmountMicros, nullable OrderID, status/quota */ }
type TronTopupTicket struct { /* ClaimKey unique, user/order/deposit, reason/status/notes/resolver audit */ }
type TronScanCheckpoint struct { /* Name primary key, LastScannedMS, UpdatedAt */ }
```

Use portable GORM types (`bigint`, bounded `varchar`, `text`), no partial indexes and no dialect-specific SQL. Add all four to normal and fast migration lists. Add `PaymentMethodTron` / `PaymentProviderTron` constants.

**Step 4: Implement the atomic model API**

Required exported operations:

```go
func CreateTronTopupOrder(topUp *TopUp, order *TronTopupOrder) error
func SettleTronDeposit(transfer TronTransferRecord, callerIP string) (TronSettlementResult, error)
func RecordUnmatchedTronDeposit(transfer TronTransferRecord) (*TronDeposit, error)
func SubmitTronTopupClaim(userID int, tradeNo, txID, note string) (*TronTopupTicket, error)
func ResolveTronTopupTicket(ticketID int64, verified TronTransferRecord, resolverID int, note string) error
```

`SettleTronDeposit` must lock the generic and TRON order rows with `lockForUpdate`, re-check provider/status and every immutable field, insert the unique txid, update user quota, and complete both records inside one `DB.Transaction`. A replay returns an explicit idempotent result. Never call the legacy unverified `ManualCompleteTopUp` path.

**Step 5: Verify GREEN and full model package**

Run `go test ./model -run 'Tron' -count=1`, then `go test ./model -count=1`. Expected: PASS.

**Step 6: Commit**

```bash
git add model/tron_topup.go model/tron_topup_test.go model/topup.go model/main.go
git commit -m "feat: add idempotent TRON top-up ledger"
```

### Task 3: Order service, scanner, checkpoint replay, and automatic tickets

**Files:**
- Create: `service/tron_topup.go`
- Create: `service/tron_topup_test.go`
- Create: `service/tron_scanner.go`
- Create: `service/tron_scanner_test.go`
- Modify: `main.go`

**Step 1: Write failing service tests**

Cover:

- configuration is disabled unless address is valid and feature flag is true;
- one unexpired order per user is reused;
- stale price fails closed and creates no rows;
- minimum/maximum amount and quota overflow are rejected;
- tail allocation retries a unique collision without changing other data;
- a transfer in a block at exactly `expires_at` settles; one millisecond later becomes a `late_payment` ticket;
- unmatched transfer is stored but does not credit;
- settlement validation failure creates one `credit_failed` ticket;
- a scan only advances checkpoint after every fingerprint page succeeds;
- partial-page failure replays safely on the next scan;
- scanner starts only on master and only when enabled.

Inject clock, random tail source, price client and chain client through small interfaces. No sleeps in tests.

**Step 2: Verify RED**

Run `go test ./service -run 'Tron' -count=1`. Expected: FAIL for missing order/scanner service.

**Step 3: Implement order creation**

Use existing `getPayMoney` semantics through a controller-provided calculation or a stable exported payment calculation function, then freeze `CreditQuota` using `common.QuotaFromDecimalChecked/Strict`. Generate `TRONUSR...` trade numbers, allocate a global never-reused 1–9999 micro-tail, and create both rows atomically. Defaults: 20-minute order and maximum CNY amount 2,000. The effective maximum is the smaller of configuration and `common.MaxQuota / QuotaPerUnit`; environment variables can only tighten, never bypass, the hard quota cap. Store quote and expiry times in milliseconds.

**Step 4: Implement scanner**

The master-only background loop runs once immediately and then every configured interval (minimum 15 seconds, default 30). Initial checkpoint is `now-10m`. Each scan overlaps the previous checkpoint by 2 minutes, groups transfers by txid, records unmatched receipts, routes exact/on-time matches to settlement, and routes late/invalid settlement to unique tickets. Advance checkpoint to the scan’s fixed upper bound only after all pages and records finish successfully. External temporary errors retain the old checkpoint and increase the loop's bounded exponential backoff with jitter; success resets the interval.

**Step 5: Verify GREEN**

Run targeted tests, `go test ./service -count=1`, then Docker `go test -race ./service -run 'Tron' -count=1`. Expected: PASS.

**Step 6: Commit**

```bash
git add service/tron_topup.go service/tron_topup_test.go service/tron_scanner.go service/tron_scanner_test.go main.go
git commit -m "feat: scan and settle confirmed TRON deposits"
```

### Task 4: Authenticated user/admin APIs and payment-info integration

**Files:**
- Create: `controller/topup_tron.go`
- Create: `controller/topup_tron_test.go`
- Modify: `controller/topup.go`
- Modify: `router/api-router.go`

**Step 1: Write failing HTTP behavior tests**

Build Gin test routers with real SQLite model behavior and fake price/chain boundaries. Cover unauthenticated/other-user access, create/reuse, status, 64-hex txid validation, note length, duplicate claim, admin-only listing, admin re-verification, reject/resolve audit, and legacy admin completion rejecting TRON provider.

**Step 2: Verify RED**

Run `go test ./controller -run 'Tron' -count=1`. Expected: FAIL.

**Step 3: Implement routes and validation**

User routes:

```text
POST /api/user/tron/topup/orders
GET  /api/user/tron/topup/orders/:trade_no
POST /api/user/tron/topup/claims
```

Admin routes:

```text
GET  /api/user/tron/topup/tickets
POST /api/user/tron/topup/tickets/:id/resolve
POST /api/user/tron/topup/tickets/:id/reject
GET  /api/user/tron/topup/status
```

Apply `UserAuth`/`AdminAuth`, `CriticalRateLimit`, `UserCriticalRateLimit`, and `DisableCache` where appropriate. Parse integer amounts only, cap note lengths before logging, return generic external-service errors, and never return API keys. `GetTopUpInfo` appends `USDT (TRON/TRC20)` only when payment compliance and valid TRON config are both enabled.

**Step 4: Verify GREEN and regression packages**

Run targeted controller tests, then `go test ./controller ./router ./model ./service -count=1`. Expected: PASS.

**Step 5: Commit**

```bash
git add controller/topup_tron.go controller/topup_tron_test.go controller/topup.go router/api-router.go
git commit -m "feat: expose TRON top-up and recovery APIs"
```

### Task 5: Dedicated React payment/status/claim experience

**Files:**
- Modify: `web/src/features/wallet/types.ts`
- Modify: `web/src/features/wallet/api.ts`
- Modify: `web/src/features/wallet/constants.ts`
- Create: `web/src/features/wallet/hooks/use-tron-payment.ts`
- Create: `web/src/features/wallet/hooks/__tests__/use-tron-payment.test.ts`
- Modify: `web/src/features/wallet/hooks/index.ts`
- Create: `web/src/features/wallet/components/dialogs/tron-payment-dialog.tsx`
- Create: `web/src/features/wallet/components/dialogs/__tests__/tron-payment-dialog.test.tsx`
- Modify: `web/src/features/wallet/components/recharge-form-card.tsx`
- Modify: `web/src/features/wallet/index.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: other locale JSON files via `bun run i18n:sync`

**Step 1: Apply relevant UI guidance**

Before editing, query the UI/UX skill for `fintech payment QR dialog`, accessibility, mobile modal layout, and React/Tailwind guidance. Read the relevant Vercel rules for effect dependencies, functional state updates, conditional rendering, and bundle imports.

**Step 2: Write failing hook and dialog tests**

Use Bun/Vitest-compatible tests in module `__tests__` directories. Cover:

- TRON button bypasses redirect payment and creates an order;
- dialog exposes network, exact amount, address, locked rate and expiry using accessible labels;
- copy actions copy only the intended value;
- pending polling stops on close/unmount and on success;
- success refreshes wallet balance;
- expired/unmatched states expose claim form;
- invalid txid and API failure show translated errors;
- 320px layout has no forced horizontal overflow and buttons remain keyboard accessible.

**Step 3: Verify RED**

Run:

```bash
cd web
/Users/apple/Library\ Application\ Support/kiro-cli/bun test \
  src/features/wallet/hooks/__tests__/use-tron-payment.test.ts \
  src/features/wallet/components/dialogs/__tests__/tron-payment-dialog.test.tsx
```

Expected: FAIL because the hook/dialog do not exist.

**Step 4: Implement API, state and UI**

Use typed API responses. The dialog contains a QR code, monospace exact amount/address, copy buttons, live countdown derived from `expires_at`, explicit `TRON / TRC20 / official USDT only` warning, and claim form. Use existing Base UI dialog/button/input/alert components and theme tokens; no raw HTML, no secrets/localStorage, no decorative emoji, and no scale-based hover shift. Poll only while open and pending, clean intervals in effects, and keep callbacks stable.

**Step 5: Sync i18n and verify GREEN**

Run targeted tests, `bun run i18n:sync`, lint only changed files, `bun run typecheck`, and `bun run build`. The known unrelated `encodeChannelConnectionInfo` baseline error may still appear; all new/changed files must be clean and the final report must distinguish it.

**Step 6: Commit**

```bash
git add web/src/features/wallet web/src/i18n/locales
git commit -m "feat: add TRON payment and claim dialog"
```

### Task 6: Production operations, monitoring, and runbook

**Files (operations repository `/Users/apple/new api`):**
- Create: `scripts/tron_topup_admin.py`
- Create: `scripts/test_tron_topup_admin.py`
- Create: `scripts/tron_topup_health.py`
- Create: `scripts/test_tron_topup_health.py`
- Modify: `docker-compose.yml`
- Modify: `README.md`

**Step 1: Write failing Python tests**

Stub `na` and network access. Cover status age thresholds, open `credit_failed` ticket alerts, redaction of keys/tx details, paginated ticket listing, resolve/reject requiring explicit order/ticket identifiers, dry-run default, and non-zero exit on ambiguous API responses.

Run:

```bash
cd '/Users/apple/new api/scripts'
python3 -m pytest test_tron_topup_admin.py test_tron_topup_health.py -q
```

Expected: FAIL because scripts are absent.

**Step 2: Implement safe operations tools**

`tron_topup_admin.py` lists tickets by default; mutations require `--resolve`/`--reject`, an explicit ticket ID, admin note, and `--commit`. It calls only the new verified admin APIs. `tron_topup_health.py` checks enabled/config state, checkpoint freshness, last successful scan, unresolved credit failures, and exits with the wrapper-compatible alert code. Never print API keys; txid display is bounded/redacted unless an exact ticket is requested.

**Step 3: Wire compose and runbook**

Add public/non-secret environment values:

```yaml
- TRON_TOPUP_ENABLED=true
- TRON_NETWORK=mainnet
- TRON_RECEIVE_ADDRESS=TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f
- TRON_TOPUP_ORDER_TTL_MINUTES=20
- TRON_TOPUP_SCAN_INTERVAL_SECONDS=30
```

Document optional root-only `.env` entries `TRONGRID_API_KEY` and `COINGECKO_API_KEY`, health cron wiring, ticket SOP, snapshot/build/deploy/rollback sequence, and real low-value mainnet acceptance.

**Step 4: Verify GREEN and commit ops changes**

Run targeted pytest, `python3 scripts/verify.py` if applicable, `docker compose config`, and `git diff --check`. Commit only operations files; planning files remain task artifacts unless intentionally included.

### Task 7: Security review, full verification, deployment, and live acceptance

**Files:**
- Modify only files required by review findings.

**Step 1: Run automated verification**

- Go targeted tests and `-race` for TRON packages.
- Root and relaykit full tests in Go 1.26.1 Docker.
- `gofmt`, `go vet` on affected packages, and `git diff --check`.
- Frontend targeted tests, affected-file lint, typecheck, format check and production build.
- Operations pytest and `docker compose config`.
- Search diffs for secrets, private keys, unbounded bodies, unsafe URLs, raw SQL, bare quota casts, logs containing credentials, and unauthenticated mutation routes.

**Step 2: Request independent code and security review**

Use the required code-reviewer; because this handles payment, user input, API endpoints and credentials, also use the security-reviewer. Fix all high/medium issues with a new failing regression test before changing implementation, then rerun the affected and full suites.

**Step 3: Prepare production atomically**

On VPS: verify clean source/ops trees, create DB and compose snapshots, fetch the feature branch, build a new immutable `new-api-custom:vNN-tron-usdt` tag without replacing the running container, inspect image metadata, update the three synchronized image-tag references required by the ops runbook, and run `docker compose config`.

**Step 4: Deploy and verify without bypassing compose**

Use only `docker compose up -d`. Confirm MySQL/Redis/new-api health, migration-created tables/indexes, public `topup/info` shows TRON to an authenticated test user, scanner checkpoint advances, and existing Epay/Alipay remains unchanged. Run existing channel/runtime/container guards and the new TRON health check.

**Step 5: Perform real smallest-value mainnet acceptance**

Create a minimum TRON order using a dedicated test user, send the exact displayed official TRC20-USDT amount to the configured address, record the txid privately, wait for solidification and verify exactly one quota increase, success order/deposit linkage, unchanged result after replay, and no open failure ticket. Then exercise ticket submission on a non-crediting test fixture without sending a second real transfer.

If no funded sending wallet is available, deployment may be technically complete but must be reported as **not live-accepted**; do not claim end-to-end completion until the real transaction succeeds.

**Step 6: Finalize and document rollback**

Rollback reverts compose to the previous image tag and runs `docker compose up -d`; new tables are retained inert for audit and must not be destructively dropped. Verify old payment paths, health checks and balances after rollback. Update both repositories’ README/progress, persistent memory, and final test evidence.
