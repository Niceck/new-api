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
// What may follow the site name in a static title: optional spaces, then a
// separator. A bare space is not enough, or shortening the name in settings
// ("CC API" -> "CC") would keep the old title.
const SEPARATOR = /^\s*[-–—|｜·:：]/

/**
 * Decide the tab title once the site name is known.
 *
 * A deployment can ship a full, keyword-bearing <title> in index.html
 * ("<site name> - <tagline>"). Search engines index the rendered title, so
 * replacing it with the bare site name throws the tagline away. Keep the
 * static title when it already leads with the site name; any other static
 * title (the stock "New API", or one written for a name that has since been
 * changed in settings) is replaced with the site name, as upstream does.
 */
export function resolveDocumentTitle(
  staticTitle: string,
  systemName: string
): string {
  const title = staticTitle.trim()
  const name = systemName.trim()
  if (!name) return title
  if (title === name) return name
  const leadsWithName =
    title.startsWith(name) && SEPARATOR.test(title.slice(name.length))
  return leadsWithName ? title : name
}
