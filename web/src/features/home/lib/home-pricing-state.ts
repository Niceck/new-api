/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { CurrencyConfig } from '@/stores/system-config-store'

import type { PriceLine } from './price-table'

/**
 * When the home page may show prices.
 *
 * Amounts are formatted with the site's display currency, which lives in a
 * store that starts out as USD until /api/status has been read. Prices can
 * arrive first, so nothing numeric is shown until the currency is confirmed:
 * a yuan price under a dollar sign is a wrong price.
 */

type MoneyDisplay = Pick<
  CurrencyConfig,
  | 'quotaDisplayType'
  | 'usdExchangeRate'
  | 'customCurrencySymbol'
  | 'customCurrencyExchangeRate'
>

/**
 * The stored currency, once /api/status (fresh or cached) agrees with it on
 * everything that changes how an amount is written; null until then.
 */
export function confirmCurrency<T extends MoneyDisplay>(
  fromStatus: MoneyDisplay | null,
  stored: T
): T | null {
  if (!fromStatus) return null
  const agrees =
    fromStatus.quotaDisplayType === stored.quotaDisplayType &&
    fromStatus.usdExchangeRate === stored.usdExchangeRate &&
    fromStatus.customCurrencySymbol === stored.customCurrencySymbol &&
    fromStatus.customCurrencyExchangeRate === stored.customCurrencyExchangeRate
  return agrees ? stored : null
}

type CurrencyWait = {
  lines: PriceLine[]
  pricingLoading: boolean
  /** Confirmed display currency, see confirmCurrency */
  currency: CurrencyConfig | null
  statusFetching: boolean
}

export type HomePricingInput = CurrencyWait & {
  /**
   * The wait for the currency has run past its deadline. The status request
   * has no timeout of its own, so without this a hung request would leave
   * the price section loading forever.
   */
  currencyWaitExpired: boolean
}

export type HomePricing = {
  /** Empty unless prices and currency are both in hand */
  lines: PriceLine[]
  /** Set exactly when `lines` is non-empty */
  currency: CurrencyConfig | null
  isLoading: boolean
  /** Settled without anything that can be shown as a price */
  isUnavailable: boolean
}

/**
 * Prices are in hand and only the currency is missing, with the status
 * request still under way. Nothing else is worth waiting for: once the price
 * request has settled without lines, the currency no longer matters.
 */
export function isAwaitingCurrency(input: CurrencyWait): boolean {
  return (
    !input.pricingLoading &&
    input.lines.length > 0 &&
    input.currency === null &&
    input.statusFetching
  )
}

export function resolveHomePricing(input: HomePricingInput): HomePricing {
  const waitingForCurrency =
    isAwaitingCurrency(input) && !input.currencyWaitExpired
  const isLoading = input.pricingLoading || waitingForCurrency
  // A currency confirmed after the deadline still counts: prices come back.
  const isReady =
    !isLoading && input.currency !== null && input.lines.length > 0

  return {
    lines: isReady ? input.lines : [],
    currency: isReady ? input.currency : null,
    isLoading,
    isUnavailable: !isLoading && !isReady,
  }
}
