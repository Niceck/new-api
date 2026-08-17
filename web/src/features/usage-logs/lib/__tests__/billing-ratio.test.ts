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

import { getLogGroupRatio } from '../billing-ratio'

describe('log group ratio', () => {
  test('prefers the user-exclusive ratio over the group ratio', () => {
    const result = getLogGroupRatio({ group_ratio: 0.7, user_group_ratio: 0.5 })

    assert.equal(result.ratio, 0.5)
    assert.equal(result.isUserExclusive, true)
    assert.equal(result.multiplier, 0.5)
  })

  test('falls back to the group ratio when the exclusive ratio is the -1 sentinel', () => {
    const result = getLogGroupRatio({ group_ratio: 1.75, user_group_ratio: -1 })

    assert.equal(result.ratio, 1.75)
    assert.equal(result.isUserExclusive, false)
    assert.equal(result.multiplier, 1.75)
  })

  test('falls back to the group ratio when the exclusive ratio is not finite', () => {
    const result = getLogGroupRatio({
      group_ratio: 0.7,
      user_group_ratio: Number.NaN,
    })

    assert.equal(result.ratio, 0.7)
    assert.equal(result.isUserExclusive, false)
    assert.equal(result.multiplier, 0.7)
  })

  test('keeps a zero group ratio as a zero multiplier instead of neutralising it', () => {
    const result = getLogGroupRatio({ group_ratio: 0 })

    assert.equal(result.ratio, 0)
    assert.equal(result.multiplier, 0)
  })

  test('reports a neutral multiplier when the log carries no ratio at all', () => {
    const result = getLogGroupRatio({})

    assert.equal(result.ratio, null)
    assert.equal(result.isUserExclusive, false)
    assert.equal(result.multiplier, 1)
  })

  test('reports a neutral multiplier for a missing other payload', () => {
    const result = getLogGroupRatio(null)

    assert.equal(result.ratio, null)
    assert.equal(result.multiplier, 1)
  })
})
