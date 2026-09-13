# TRON payment hardening (r5)

Approved scope: repair and deploy the September 13 payment audit findings.
Baseline: c4ad7d61 / new-api-custom:v14-tron-usdt-r4.

## Settlement

Account-history rows discover candidate transaction hashes only. Solidified
execution receipts supply each official USDT Transfer event exactly once.
Successful-order second payments and multi-source receipts are persisted as
review deposits without automatic credit. Deposit hashes stay globally unique;
manual resolution uses the same receipt verification and preserves all sources.
Existing quotas, locked rates, amounts, wallet address and contract stay unchanged.

## Scanner and availability

Normal scanning has a durable fixed cycle, cursor, and settlement watermark;
the two-minute overlap is applied only when starting a new cycle. Each fully
persisted subwindow advances the cursor, including overlap below the watermark.
A separate durable cycle covers the hourly 25-hour reconciliation.

Windows start at 15 minutes. Page-cap or subwindow timeout halves the window,
persisting the new span before retry. Successful windows gradually grow back.
A one-millisecond window that still fails is reported, never skipped. The round
budget is 60 seconds and window/request budgets are 10 seconds.

New orders require a scanner watermark no older than ten minutes and successful
scan update no older than six minutes. Missing/future timestamps fail closed.
Existing orders can still be queried and claimed. Quotes are coalesced and cached
for sixty seconds while enforcing the original six-minute source freshness cap.

## Operations and UI

Admin-only GET /api/user/tron/topup/deposits provides bounded status pagination.
Health adds unmatched count/amount/age, review count, order readiness and wide
reconciliation timestamps. CLI --deposits is read-only and redacts txids.
Unmatched funds older than ten minutes and review funds generate alerts.

Order views add claim_deadline_ms, can_claim and review_status. Expired orders
continue thirty-second polling until the 24-hour claim deadline, with manual
refresh after that. History opens the original user-owned order without creating
another. Expired/successful orders hide payable QR codes. TRON admin rows never
invoke the generic completion endpoint. All new copy supports seven locales.

## Validation and rollout

Regression tests cover second payments, receipt duplication and identity,
multiple sources, quota idempotence, timeouts, persistent subdivision, interrupted
overlap and reconciliation, order gates, quote cache and late UI updates.
Run Go full tests/vet, TRON race tests, frontend tests/typecheck/lint/build,
ops tests and isolated MySQL/PostgreSQL migrations and settlement tests.

Build a Linux binary from the reviewed source and embedded frontend. Layer it
onto the exact r4 runtime image, record SHA256 and source revision, and publish
as new-api-custom:v14-tron-usdt-r5. Preserve predeploy encrypted full backup,
source/ops archives and r4 image ID. Compose alone replaces the production
container. Check runtime health, migrations, checkpoints, API authorization,
existing payment methods and the natural ops cycle.

Rollback to r4 MUST disable TRON first: r4 cannot safely scan the newly handled
multi-source/second-payment scenarios. Keep the ledger and audit columns; never
DROP financial data. Restore TRON only using a corrected image. A real small
mainnet payment remains a user-owned external-wallet acceptance step; software
checks do not replace that acceptance.
