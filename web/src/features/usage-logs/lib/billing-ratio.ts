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
import type { LogOtherData } from '../types'

/**
 * Group ratio that was actually applied when the log was settled.
 *
 * The backend multiplies every billing path by this ratio — per-token,
 * per-call, tiered expressions and tool-call surcharges alike (see
 * `relay/helper/price.go` and `service/text_quota.go`). Prices rendered from a
 * log must include it too, otherwise the log contradicts both the amount
 * charged and the model square, which already multiplies by the group ratio.
 *
 * A user-exclusive ratio replaces the group ratio; the backend writes `-1`
 * when no exclusive ratio applies.
 */
export type LogGroupRatio = {
  /** Ratio to display, or null when the log carries none. */
  ratio: number | null
  /** Whether the ratio comes from a user-exclusive override. */
  isUserExclusive: boolean
  /** Factor to apply to displayed prices; 1 when the log carries no ratio. */
  multiplier: number
}

export function getLogGroupRatio(
  other: LogOtherData | null | undefined
): LogGroupRatio {
  const userRatio = other?.user_group_ratio
  if (userRatio != null && Number.isFinite(userRatio) && userRatio !== -1) {
    return { ratio: userRatio, isUserExclusive: true, multiplier: userRatio }
  }

  const groupRatio = other?.group_ratio
  // A zero group ratio is a real free-of-charge configuration, so it must stay
  // a zero multiplier instead of collapsing to the neutral 1.
  if (groupRatio != null && Number.isFinite(groupRatio)) {
    return { ratio: groupRatio, isUserExclusive: false, multiplier: groupRatio }
  }

  return { ratio: null, isUserExclusive: false, multiplier: 1 }
}
