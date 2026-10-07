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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { stringToColor } from '@/lib/colors'

import { getPinnedGroupColor } from '../colors'

// The `stringToColor` assertions on the previous group keys record where each
// pinned color comes from; they are not how the expected value is computed.
describe('group badge pinned colors', () => {
  test('pins the main Claude line to the cyan its previous key produced', () => {
    assert.equal(stringToColor('超级稳定claude'), 'cyan')
    assert.equal(getPinnedGroupColor('满血稳定claude'), 'cyan')
  })

  test('pins the Codex line to the lime its previous key produced', () => {
    assert.equal(stringToColor('Codex PRO'), 'lime')
    assert.equal(getPinnedGroupColor('满血稳定codex'), 'lime')
  })

  test('leaves the kiro line unpinned so it keeps its name-derived red', () => {
    assert.equal(getPinnedGroupColor('claude kiro'), undefined)
    assert.equal(stringToColor('claude kiro'), 'red')
  })

  test('returns no pinned color for groups that are not listed', () => {
    for (const group of ['default', 'vip', 'auto', '']) {
      assert.equal(getPinnedGroupColor(group), undefined, group)
    }
  })

  test('does not mistake inherited object keys for pinned groups', () => {
    for (const group of ['constructor', 'toString', '__proto__']) {
      assert.equal(getPinnedGroupColor(group), undefined, group)
    }
  })

  test('matches group names exactly, without folding case', () => {
    assert.equal(getPinnedGroupColor('满血稳定Claude'), undefined)
    assert.equal(getPinnedGroupColor('满血稳定CODEX'), undefined)
  })
})
