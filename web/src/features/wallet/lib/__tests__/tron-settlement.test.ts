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

import { hasTronTolerance, tronCreditedAmount } from '../tron'

describe('tronCreditedAmount', () => {
  test('scales the face amount by what was actually credited', () => {
    assert.equal(
      tronCreditedAmount({
        amount: 100,
        tron_received_usdt_micros: 13_952_525,
        tron_credited_quota: 46_656_083,
        tron_face_quota: 50_000_000,
      }),
      (100 * 46_656_083) / 50_000_000
    )
  })

  test('returns the face amount when the full face value was credited', () => {
    assert.equal(
      tronCreditedAmount({
        amount: 100,
        tron_received_usdt_micros: 15_018_578,
        tron_credited_quota: 50_000_000,
        tron_face_quota: 50_000_000,
      }),
      100
    )
  })

  test('returns null for records without TRON settlement facts', () => {
    assert.equal(tronCreditedAmount({ amount: 100 }), null)
    assert.equal(
      tronCreditedAmount({ amount: 100, tron_credited_quota: 1 }),
      null
    )
    assert.equal(
      tronCreditedAmount({
        amount: 100,
        tron_credited_quota: 5,
        tron_face_quota: 5,
      }),
      null
    )
  })
})

describe('hasTronTolerance', () => {
  test('needs the switch and both bounds', () => {
    assert.equal(
      hasTronTolerance({
        tolerant_match: true,
        tolerant_min_usdt_micros: 9_000_000,
        tolerant_max_usdt_micros: 11_000_000,
      }),
      true
    )
    assert.equal(hasTronTolerance({}), false)
    assert.equal(
      hasTronTolerance({
        tolerant_match: false,
        tolerant_min_usdt_micros: 9_000_000,
        tolerant_max_usdt_micros: 11_000_000,
      }),
      false
    )
    assert.equal(hasTronTolerance({ tolerant_match: true }), false)
  })
})
