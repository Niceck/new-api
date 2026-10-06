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
import {
  tryParseVisualConfig,
  type VisualTier,
} from '@/features/pricing/lib/tier-expr'

/**
 * Home page price table, derived from the public /api/pricing payload.
 *
 * The payload is treated as untrusted: anything that does not look like a
 * priced model on a group with a real ratio is dropped instead of guessed,
 * because a wrong number here is a wrong price shown to a customer.
 */

/** A model ratio of 1 is quoted at this many USD per million input tokens. */
const USD_PER_MILLION_AT_RATIO_ONE = 2

/** Rough USD→CNY rate behind the "about N% of the official price" wording. */
export const USD_CNY_ROUGH = 7

const QUOTA_TYPE_TOKEN = 0
const TIERED_BILLING_MODE = 'tiered_expr'

export type OfficialQuote =
  | {
      kind: 'priced'
      /** USD per million input tokens */
      input: number
      /** USD per million output tokens */
      output: number
      /**
       * Input length (tokens) up to which this quote applies, for tiered
       * models; null when the price does not depend on input length.
       */
      inputLimit: number | null
    }
  /** Priced by rules this table cannot summarise: never guess a price. */
  | { kind: 'unsupported' }

export type PriceQuote = {
  officialInput: number
  officialOutput: number
  /** Official price × group ratio, still in the system's USD unit */
  siteInput: number
  siteOutput: number
  inputLimit: number | null
}

export type PriceRow = {
  model: string
  /** null: sold on this line, but priced by rules too involved to quote here */
  quote: PriceQuote | null
}

export type PriceLine = {
  /** Group key as used by tokens and /api/pricing */
  group: string
  /** Customer-facing group name without its trailing note */
  label: string
  ratio: number
  rows: PriceRow[]
}

export type PriceLineOptions = {
  /** Models to list, in display order */
  headlineModels: readonly string[]
  /**
   * The lines the home page may show, in display order. This is an
   * allowlist: a group the payload returns but that is not listed here
   * (an internal or admin-only line, say) never appears.
   */
  lineOrder: readonly string[]
}

type DiscountCurrency = {
  quotaDisplayType: string
  usdExchangeRate: number
}

type PricedModel = {
  name: string
  groups: string[]
  quote: OfficialQuote
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isPositiveNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

/** Drop a trailing "（…）" / "(…)" note such as the discount in a group name. */
export function stripGroupLabelNote(label: string): string {
  const stripped = label.replace(/\s*[（(][^（）()]*[）)]\s*$/, '').trim()
  return stripped || label
}

/**
 * Share of the official price a customer pays on a group, or null when it
 * cannot be stated honestly (no saving, or a display unit that is not money
 * comparable to the official USD quote).
 */
export function getDiscountFraction(
  ratio: number,
  currency: DiscountCurrency
): number | null {
  if (!isPositiveNumber(ratio)) return null

  let fraction: number | null = null
  if (currency.quotaDisplayType === 'USD') {
    fraction = ratio
  } else if (
    currency.quotaDisplayType === 'CNY' &&
    isPositiveNumber(currency.usdExchangeRate)
  ) {
    fraction = (ratio * currency.usdExchangeRate) / USD_CNY_ROUGH
  }

  if (fraction === null || fraction >= 1) return null
  return fraction
}

function toOneDecimal(value: number): string {
  return Number(value.toFixed(1)).toString()
}

/**
 * A discount fraction in the two ways it is worded: "N 折" (tenths, zh) and
 * a percentage of the official price (other languages).
 */
export function formatDiscount(fraction: number): {
  zhe: string
  percent: string
} {
  return {
    zhe: toOneDecimal(fraction * 10),
    percent: toOneDecimal(fraction * 100),
  }
}

const UNSUPPORTED: OfficialQuote = { kind: 'unsupported' }

function isPositiveInteger(value: unknown): value is number {
  return isPositiveNumber(value) && Number.isInteger(value)
}

/**
 * Largest input length that still falls in a tier bounded above by
 * `len <= value` or `len < value` (token counts are whole numbers).
 */
function inclusiveInputLimit(op: string, value: unknown): number | null {
  if (!isPositiveInteger(value)) return null
  if (op === '<=') return value
  if (op === '<' && value > 1) return value - 1
  return null
}

/**
 * The tier to quote, with the input length it applies up to. Only two
 * shapes are summarised: a single unconditional tier, or several tiers led
 * by one bounded above on input length (the price of an ordinary request).
 */
function pickQuotedTier(
  tiers: VisualTier[]
): { tier: VisualTier; inputLimit: number | null } | null {
  const [first] = tiers
  if (!first) return null
  if (tiers.length === 1) {
    return first.conditions.length === 0
      ? { tier: first, inputLimit: null }
      : null
  }
  if (first.conditions.length !== 1) return null
  const [condition] = first.conditions
  if (condition.var !== 'len') return null
  const inputLimit = inclusiveInputLimit(condition.op, condition.value)
  return inputLimit === null ? null : { tier: first, inputLimit }
}

function getTieredQuote(expr: unknown): OfficialQuote {
  if (typeof expr !== 'string') return UNSUPPORTED
  // Strict on purpose: this only accepts expressions that round-trip through
  // the tier editor's format, so request rules, extra factors and anything
  // hand-written fall through to "unsupported" instead of a partial reading.
  const config = tryParseVisualConfig(expr)
  const quoted = config ? pickQuotedTier(config.tiers) : null
  if (!quoted) return UNSUPPORTED
  const input = quoted.tier.input_unit_cost
  const output = quoted.tier.output_unit_cost
  if (!isPositiveNumber(input) || !isPositiveNumber(output)) {
    return UNSUPPORTED
  }
  return { kind: 'priced', input, output, inputLimit: quoted.inputLimit }
}

/**
 * Official USD quote per million tokens for one /api/pricing model entry,
 * or null when the entry is not a token-priced model we can describe.
 */
export function getOfficialQuote(model: unknown): OfficialQuote | null {
  if (!isRecord(model)) return null
  if (model.quota_type !== QUOTA_TYPE_TOKEN) return null

  const billingMode = model.billing_mode
  if (billingMode === TIERED_BILLING_MODE) {
    return getTieredQuote(model.billing_expr)
  }
  // The backend only ever sends "tiered_expr" or nothing. Any other mode is
  // one this table does not know how to price.
  const usesPlainRatios =
    billingMode === undefined || billingMode === null || billingMode === ''
  if (!usesPlainRatios) return UNSUPPORTED

  const modelRatio = model.model_ratio
  const completionRatio = model.completion_ratio
  if (!isPositiveNumber(modelRatio) || !isPositiveNumber(completionRatio)) {
    return null
  }
  const input = modelRatio * USD_PER_MILLION_AT_RATIO_ONE
  const output = input * completionRatio
  if (!isPositiveNumber(input) || !isPositiveNumber(output)) return null
  return { kind: 'priced', input, output, inputLimit: null }
}

function readGroupRatios(payload: unknown): Map<string, number> {
  const ratios = new Map<string, number>()
  if (!isRecord(payload) || !isRecord(payload.group_ratio)) return ratios
  for (const [group, ratio] of Object.entries(payload.group_ratio)) {
    if (isPositiveNumber(ratio)) ratios.set(group, ratio)
  }
  return ratios
}

function readGroupLabel(payload: unknown, group: string): string {
  if (!isRecord(payload) || !isRecord(payload.usable_group)) return group
  const raw = payload.usable_group[group]
  if (typeof raw === 'string' && raw.trim()) return stripGroupLabelNote(raw)
  if (isRecord(raw) && typeof raw.desc === 'string' && raw.desc.trim()) {
    return stripGroupLabelNote(raw.desc)
  }
  return group
}

function readPricedModels(
  payload: unknown,
  ratios: Map<string, number>
): PricedModel[] {
  if (!isRecord(payload) || !Array.isArray(payload.data)) return []
  return payload.data.flatMap((entry): PricedModel[] => {
    if (!isRecord(entry) || typeof entry.model_name !== 'string') return []
    const groups = toStringArray(entry.enable_groups).filter((group) =>
      ratios.has(group)
    )
    const quote = getOfficialQuote(entry)
    if (groups.length === 0 || !quote) return []
    return [{ name: entry.model_name, groups, quote }]
  })
}

function toRow(model: PricedModel, ratio: number): PriceRow {
  const unpriced: PriceRow = { model: model.name, quote: null }
  if (model.quote.kind !== 'priced') return unpriced
  const siteInput = model.quote.input * ratio
  const siteOutput = model.quote.output * ratio
  if (!isPositiveNumber(siteInput) || !isPositiveNumber(siteOutput)) {
    return unpriced
  }
  return {
    model: model.name,
    quote: {
      officialInput: model.quote.input,
      officialOutput: model.quote.output,
      siteInput,
      siteOutput,
      inputLimit: model.quote.inputLimit,
    },
  }
}

/**
 * The single input-length limit shared by every tiered quote in these lines,
 * or null when there is none or they differ (the footnote then stays general).
 */
export function getSharedInputLimit(lines: PriceLine[]): number | null {
  const limits = new Set(
    lines.flatMap((line) =>
      line.rows.flatMap((row) =>
        row.quote && row.quote.inputLimit !== null ? [row.quote.inputLimit] : []
      )
    )
  )
  const [only] = [...limits]
  return limits.size === 1 ? only : null
}

/** Lines (groups) that carry at least one headline model, with their rows. */
export function buildPriceLines(
  payload: unknown,
  options: PriceLineOptions
): PriceLine[] {
  // A body that does not say it succeeded is not a price list, whatever
  // else it carries.
  if (!isRecord(payload) || payload.success !== true) return []
  const ratios = readGroupRatios(payload)
  const models = readPricedModels(payload, ratios)
  const byName = new Map(models.map((model) => [model.name, model]))
  const headline = options.headlineModels.flatMap((name) => {
    const model = byName.get(name)
    return model ? [model] : []
  })

  return options.lineOrder.flatMap((group): PriceLine[] => {
    const ratio = ratios.get(group)
    if (ratio === undefined) return []
    const rows = headline
      .filter((model) => model.groups.includes(group))
      .map((model) => toRow(model, ratio))
    if (rows.length === 0) return []
    return [{ group, label: readGroupLabel(payload, group), ratio, rows }]
  })
}
