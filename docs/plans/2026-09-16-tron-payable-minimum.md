# TRON payable minimum and index rounding (r6)

Approved scope: make the TRON line actually payable after the September 16
finding that every order created since launch (seven orders, ¥1–¥10) had zero
on-chain deposits. Baseline: 74c280a3 / new-api-custom:v14-tron-usdt-r5.

## Root cause

The wallet page allowed any amount from the generic minimum (¥1) for TRON.
Exchanges enforce their own USDT TRC20 withdrawal minimums (2–10 USDT) and
deduct roughly 1 USDT in network fees, so a ¥1 order (≈0.15 USDT) can never be
paid from an exchange account, and a self-custody wallet must additionally hold
TRX for energy. Users abandoned the dialog; the scanner saw nothing.

A second, latent defect: TronGrid rounds `min_timestamp` down to the containing
second. A window starting mid-second therefore also returns the previous
window's transfer from earlier in the same second, which the client rejected as
"outside requested window", wedging the scanner on that window until the next
subdivision. Verified against mainnet on 2026-09-16.

## Changes

- `TRON_TOPUP_MIN_AMOUNT` (top-up units, default 10, range 1–1,000,000).
  Enforced in the controller (user-facing message names the minimum) and again
  in the service before any price quote is consumed. TOKENS display mode scales
  the minimum by `QuotaPerUnit`, mirroring the generic minimum.
- `/api/user/topup/info` exposes `tron_min_topup` (0 when TRON is off or
  compliance is unconfirmed). The wallet card disables the TRON action below
  it; the payment dialog explains that exchange fees must not reduce the
  amount that arrives.
- TronGrid client tolerates index rows from the same second before the window
  start (skips them without receipt lookups); rows from an earlier second or
  after the window end remain contract violations.

No schema changes. Locked rates, amounts, wallet address, contract, claim and
ticket flows are unchanged. Rolling back to r5 needs no TRON disable: r5
ignores the new variable.

## Validation

Go: config bounds, sub-minimum rejection before quoting, controller minimum
and error mapping, topup-info field, same-second tolerance versus earlier
second and post-window rejection. Frontend: TRON action minimum/disabled
state, dialog fee hint. Ops: compose pins the minimum and the r6 tag.
