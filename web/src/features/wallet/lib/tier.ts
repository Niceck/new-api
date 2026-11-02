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
import type { TopupTierLevel, TopupTierStatus } from '../types'

// Cumulative top-up tiers: once the face value of a user's successful
// top-ups reaches a threshold, every later top-up (any amount) pays that
// tier's rate. The server quotes the real price; this module only shapes
// what the wallet page shows and never stacks the two discounts.

function finiteNumber(value: unknown): number | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric : null
}

function parseTierLevel(item: unknown): TopupTierLevel | null {
  if (!item || typeof item !== 'object' || Array.isArray(item)) {
    return null
  }
  const raw = item as Record<string, unknown>
  const level = finiteNumber(raw.level)
  const threshold = finiteNumber(raw.threshold)
  const discount = finiteNumber(raw.discount)
  if (level === null || threshold === null || discount === null) {
    return null
  }
  const bonus = finiteNumber(raw.bonus)
  return bonus === null
    ? { level, threshold, discount }
    : { level, threshold, discount, bonus }
}

/**
 * Shape-check the `tier` object of /api/user/topup/info.
 * Returns null when the server has no tiers configured (it sends null),
 * when the backend predates tiers (field missing) or when the payload is
 * malformed: the page then simply shows no tier block.
 */
export function parseTopupTier(data: unknown): TopupTierStatus | null {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return null
  }
  const raw = data as Record<string, unknown>
  const total = finiteNumber(raw.total)
  const level = finiteNumber(raw.level)
  const discount = finiteNumber(raw.discount)
  if (total === null || level === null || discount === null) {
    return null
  }
  const tiers = (Array.isArray(raw.tiers) ? raw.tiers : [])
    .map(parseTierLevel)
    .filter((item): item is TopupTierLevel => item !== null)
  const optional = (key: string): number | undefined => {
    const numeric = finiteNumber(raw[key])
    return numeric === null ? undefined : numeric
  }
  return {
    total,
    level,
    discount,
    threshold: optional('threshold'),
    next_level: optional('next_level'),
    next_threshold: optional('next_threshold'),
    next_discount: optional('next_discount'),
    remaining: optional('remaining'),
    tiers,
    // Bonus fields only exist in bonus mode; a discount ladder keeps its old shape
    ...(optional('bonus') !== undefined ? { bonus: optional('bonus') } : {}),
    ...(optional('next_bonus') !== undefined
      ? { next_bonus: optional('next_bonus') }
      : {}),
  }
}

/**
 * The rate a preset really pays: the better (lower) of its own discount
 * and the member tier's. The two are never multiplied together.
 */
export function effectiveDiscount(
  presetDiscount: number | undefined,
  tier: TopupTierStatus | null | undefined
): number {
  const base =
    presetDiscount !== undefined && presetDiscount > 0 ? presetDiscount : 1
  const tierRate =
    tier && tier.level > 0 && tier.discount > 0 ? tier.discount : 1
  return Math.min(base, tierRate)
}

/**
 * The bonus rate a top-up really gets: the higher of the single-order rate
 * this amount reaches and the member tier's. They are never added together.
 * 0 when bonus mode is off. The server snapshots the real figure on the order;
 * this only drives what the wallet previews.
 */
export function effectiveBonus(
  amount: number,
  amountBonus: Record<number, number> | undefined,
  tier: TopupTierStatus | null | undefined
): number {
  let rate = 0
  for (const [threshold, value] of Object.entries(amountBonus ?? {})) {
    if (amount >= Number(threshold) && value > rate) {
      rate = value
    }
  }
  const tierRate = tier && tier.level > 0 ? (tier.bonus ?? 0) : 0
  return Math.max(rate, tierRate)
}

/**
 * The single-order bonus as the brackets customers read: any amount inside a
 * bracket gets its rate, on that actual amount. The last bracket has no upper end.
 */
export function bonusRanges(
  amountBonus: Record<number, number> | undefined
): { from: number; to?: number; rate: number }[] {
  const steps = Object.entries(amountBonus ?? {})
    .map(([threshold, rate]) => ({ from: Number(threshold), rate }))
    .filter((step) => Number.isFinite(step.from) && step.rate > 0)
    .sort((a, b) => a.from - b.from)
  return steps.map((step, index) => {
    const next = steps[index + 1]
    return next ? { ...step, to: next.from - 1 } : step
  })
}

/** A bonus rate as the percentage people read: 0.035 → 3.5, without float noise. */
export function bonusPercent(rate: number): number {
  return Number((rate * 100).toFixed(2))
}

/** Whether this ladder gives bonus credit rather than a discount. */
export function isBonusTier(tier: TopupTierStatus): boolean {
  return (
    (tier.bonus ?? 0) > 0 ||
    (tier.next_bonus ?? 0) > 0 ||
    tier.tiers.some((level) => (level.bonus ?? 0) > 0)
  )
}

/**
 * Progress (0–100) from the current threshold towards the next one.
 * 100 once the top level is reached.
 */
export function tierProgress(tier: TopupTierStatus): number {
  if (tier.next_threshold === undefined) {
    return 100
  }
  const start = tier.threshold ?? 0
  const span = tier.next_threshold - start
  if (span <= 0) {
    return 100
  }
  const percent = ((tier.total - start) / span) * 100
  return Math.max(0, Math.min(100, Math.round(percent)))
}
