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
import type { StatusVariant } from '@/components/status-badge'

/**
 * Groups whose badge color is fixed instead of derived from the group name.
 *
 * A group badge normally takes its color from `stringToColor(groupName)`, so
 * renaming a group silently changes its color. These two lines were renamed on
 * 2026-10-07 and keep the color their previous keys produced
 * (超级稳定claude -> cyan, Codex PRO -> lime), so they look the way customers
 * already know them. Groups that are not listed keep the name-derived color.
 *
 * When a group is renamed again and should keep its color, register the new
 * name here and update the tests in `__tests__/`.
 */
const PINNED_GROUP_COLORS: ReadonlyMap<string, StatusVariant> = new Map<
  string,
  StatusVariant
>([
  ['满血稳定claude', 'cyan'],
  ['满血稳定codex', 'lime'],
])

/** Returns the fixed badge color of a group, or undefined when it has none. */
export function getPinnedGroupColor(group: string): StatusVariant | undefined {
  return PINNED_GROUP_COLORS.get(group)
}
