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

import type { TopupTierStatus } from '../../types'
import { effectiveDiscount, parseTopupTier, tierProgress } from '../tier'

const ladder = [
  { level: 1, threshold: 1000, discount: 0.98 },
  { level: 2, threshold: 2500, discount: 0.97 },
  { level: 3, threshold: 5000, discount: 0.95 },
  { level: 4, threshold: 10000, discount: 0.92 },
]

const vip2: TopupTierStatus = {
  total: 2735,
  level: 2,
  discount: 0.97,
  threshold: 2500,
  next_level: 3,
  next_threshold: 5000,
  next_discount: 0.95,
  remaining: 2265,
  tiers: ladder,
}

describe('parseTopupTier', () => {
  test('null, a missing field and an old backend all mean "no tier block"', () => {
    assert.equal(parseTopupTier(null), null)
    assert.equal(parseTopupTier(undefined), null)
    assert.equal(parseTopupTier([]), null)
    assert.equal(parseTopupTier({ total: 100 }), null)
    assert.equal(parseTopupTier({ total: 'x', level: 1, discount: 0.98 }), null)
  })

  test('keeps the server shape and drops malformed ladder rows', () => {
    const parsed = parseTopupTier({
      total: 2735,
      level: 2,
      discount: 0.97,
      threshold: 2500,
      next_level: 3,
      next_threshold: 5000,
      next_discount: 0.95,
      remaining: 2265,
      tiers: [...ladder, { level: 5 }, 'junk', null],
    })
    assert.deepEqual(parsed, vip2)
  })

  test('below the first level there is no current threshold but there is a next one', () => {
    const parsed = parseTopupTier({
      total: 300,
      level: 0,
      discount: 1,
      next_level: 1,
      next_threshold: 1000,
      next_discount: 0.98,
      remaining: 700,
      tiers: ladder,
    })
    assert.equal(parsed?.level, 0)
    assert.equal(parsed?.threshold, undefined)
    assert.equal(parsed?.next_threshold, 1000)
  })
})

describe('effectiveDiscount', () => {
  test('without a tier the preset discount stands', () => {
    assert.equal(effectiveDiscount(undefined, null), 1)
    assert.equal(effectiveDiscount(0.98, null), 0.98)
    assert.equal(effectiveDiscount(0.98, undefined), 0.98)
  })

  test('takes the better of the two and never multiplies them', () => {
    assert.equal(effectiveDiscount(0.98, vip2), 0.97) // tier wins
    assert.equal(effectiveDiscount(0.92, vip2), 0.92) // single-order wins
    assert.equal(effectiveDiscount(undefined, vip2), 0.97) // custom amount
    assert.equal(effectiveDiscount(0.97, vip2), 0.97) // equal: still 0.97, not 0.9409
  })

  test('level 0 gives nothing', () => {
    const none: TopupTierStatus = { ...vip2, level: 0, discount: 1 }
    assert.equal(effectiveDiscount(undefined, none), 1)
    assert.equal(effectiveDiscount(0.98, none), 0.98)
  })
})

describe('tierProgress', () => {
  test('measures from the current threshold to the next', () => {
    assert.equal(tierProgress(vip2), 9) // (2735-2500)/(5000-2500)
    assert.equal(
      tierProgress({
        total: 300,
        level: 0,
        discount: 1,
        next_level: 1,
        next_threshold: 1000,
        tiers: ladder,
      }),
      30
    )
  })

  test('is full at the top level', () => {
    assert.equal(
      tierProgress({
        total: 10190,
        level: 4,
        discount: 0.92,
        threshold: 10000,
        tiers: ladder,
      }),
      100
    )
  })
})
