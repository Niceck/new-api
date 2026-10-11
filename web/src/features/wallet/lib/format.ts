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
import { DEFAULT_DISCOUNT_RATE } from '../constants'

// ============================================================================
// Wallet-specific Formatting Functions
// ============================================================================

/**
 * Format Creem price with currency symbol (USD/EUR)
 */
export function formatCreemPrice(
  price: number,
  currency: 'USD' | 'EUR'
): string {
  const symbol = currency === 'EUR' ? '€' : '$'
  return `${symbol}${price.toFixed(2)}`
}

/**
 * Format large quota numbers with K/M suffix
 */
export function formatQuotaShort(quota: number): string {
  if (quota >= 1000000) {
    return `${(quota / 1000000).toFixed(1)}M`
  }
  if (quota >= 1000) {
    return `${(quota / 1000).toFixed(1)}K`
  }
  return quota.toString()
}

/**
 * Format currency amount that is already in local currency.
 * This is used for payment amounts that have been calculated via priceRatio.
 */
export function formatCurrency(amount: number | string): string {
  const numeric =
    typeof amount === 'number' ? amount : Number.parseFloat(String(amount))
  if (!Number.isFinite(numeric)) return '-'

  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(numeric) >= 1 ? 2 : 4,
  }).format(numeric)
}

/**
 * A discount rate expressed the two ways locales word it: `percent` is how
 * much is taken off (3 for a 0.97 rate, "3% OFF"), `fold` is the Chinese 折
 * figure, i.e. the share still paid in tenths ("9.7 折"). The caller passes
 * both to the translation and each locale uses whichever reads naturally.
 *
 * Both figures keep two decimals so a 2.5% discount does not read as 3%.
 * Returns null when the rate is not a real discount, and also when rounding
 * would make a real one read as "no discount" or "free" (0.9999 → "10 折",
 * 0.0001 → "0 折"): the card then shows the amounts without a badge.
 */
export function getDiscountParts(
  discount: number
): { percent: number; fold: string } | null {
  if (
    !Number.isFinite(discount) ||
    discount <= 0 ||
    discount >= DEFAULT_DISCOUNT_RATE
  ) {
    return null
  }
  // toFixed first: 0.97 * 10 is 9.700000000000001 in floating point
  const percent = Number(((1 - discount) * 100).toFixed(2))
  const fold = Number((discount * 10).toFixed(2))
  if (percent <= 0 || percent >= 100 || fold <= 0 || fold >= 10) {
    return null
  }
  return { percent, fold: String(fold) }
}

/**
 * Calculate pricing details for a preset amount
 */
export function calculatePresetPricing(
  presetValue: number,
  priceRatio: number,
  discount: number,
  usdExchangeRate: number = 1
) {
  const originalPrice = presetValue * priceRatio
  const actualPrice = originalPrice * discount
  const savedAmount = originalPrice - actualPrice
  const hasDiscount = discount < 1.0
  const displayValue = presetValue * usdExchangeRate

  return {
    displayValue,
    originalPrice,
    actualPrice,
    savedAmount,
    hasDiscount,
  }
}
