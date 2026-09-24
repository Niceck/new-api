# CNY cent ceiling billing implementation

Goal: charge all newly billed requests in CNY cents rounded upward; preserve original consume log quota and price details. Wallet, subscription and token use the same charged quota. Historical logs/tasks stay unchanged.

Architecture: common.CNYCentRounding snapshots the enabled flag, USD-to-CNY rate and quota-per-unit. Exact decimal quotient/remainder computes ceil(raw * rate * 100 / quotaPerUnit), then ceil(cents * quotaPerUnit / (rate * 100)). common.BillingCharge records raw and charged quotas. Request sessions accept raw totals; preconsume/reserve/settle apply the policy only to totals. Refunds use persisted charged amounts, never rounded differences.

1. Tests: common rounding boundaries and invalid values; service wallet/subscription/preconsume/reserve/idempotent settle; task differential settlement/refund and snapshot stability.
2. Common policy and quota_setting.round_charge_to_cny_cent (default false); request snapshot and audit metadata under other.charge_rounding.
3. Session and fallback paths; use actual amounts for user statistics; cumulative realtime reservations; MJ direct charges and refunds; violation fees.
4. Async task snapshot persistence; update raw metadata even if new raw total remains within same cent; legacy tasks without snapshot retain old billing.
5. Run common/service/model/relay/controller regression tests and root build; independent review; publish r10 with option enabled after backup and health/accounting checks.

API: request formats unchanged; additive log metadata and task private snapshot. MJ needs additive nullable billing snapshot and token identity for accurate new-task refunds. No data rewriting or historical rebilling.
