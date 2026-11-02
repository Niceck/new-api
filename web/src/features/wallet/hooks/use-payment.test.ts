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

import { PAYMENT_TYPES } from '../constants'
import type { AmountResponse } from '../types'
import {
  latestOnly,
  paymentErrorMessage,
  requestPaymentAmount,
  requestPaymentQuote,
} from './use-payment'

describe('payment amount routing', () => {
  test('uses the dedicated Waffo amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(120, PAYMENT_TYPES.WAFFO, {
      regular: async () => {
        calls.push('regular')
        return { success: true, data: '1' }
      },
      stripe: async () => {
        calls.push('stripe')
        return { success: true, data: '2' }
      },
      waffo: async (request) => {
        calls.push(`waffo:${request.amount}`)
        return { success: true, data: '18.75' }
      },
      waffoPancake: async () => {
        calls.push('pancake')
        return { success: true, data: '4' }
      },
    })

    assert.equal(amount, 18.75)
    assert.deepEqual(calls, ['waffo:120'])
  })
})

describe('payment quote', () => {
  const calculators = (response: AmountResponse) => {
    const reply = async () => response
    return { regular: reply, stripe: reply, waffo: reply, waffoPancake: reply }
  }

  test('carries the bonus the server quoted', async () => {
    const quote = await requestPaymentQuote(
      500,
      'alipay',
      calculators({
        success: true,
        data: '500.00',
        bonus_rate: 0.035,
        bonus_quota: 8_750_000,
        bonus_amount: 17.5,
        credit_amount: 517.5,
      })
    )
    assert.deepEqual(quote, {
      money: 500,
      moneyText: '500.00',
      bonusRate: 0.035,
      bonusAmount: 17.5,
      bonusQuota: 8_750_000,
      creditAmount: 517.5,
      quotedFor: 500,
    })
  })

  test('no bonus fields means no bonus', async () => {
    const quote = await requestPaymentQuote(
      100,
      'alipay',
      calculators({ success: true, data: '100.00' })
    )
    assert.deepEqual(quote, {
      money: 100,
      moneyText: '100.00',
      bonusRate: 0,
      bonusAmount: 0,
      bonusQuota: 0,
      creditAmount: 0,
      quotedFor: 100,
    })
  })

  test('a failed quote is all zeros, so no 0-yuan dialog opens', async () => {
    const quote = await requestPaymentQuote(
      100,
      'alipay',
      calculators({ success: false, data: '', message: 'error' })
    )
    assert.equal(quote.money, 0)
    assert.equal(
      await requestPaymentAmount(
        100,
        'alipay',
        calculators({ success: true, data: '100.00' })
      ),
      100
    )
  })
})

describe('latestOnly', () => {
  test('a late answer for an older request is not the latest', () => {
    const next = latestOnly()
    const first = next()
    assert.equal(first(), true)
    const second = next()
    assert.equal(
      first(),
      false,
      '2000 was typed first: its answer must be dropped'
    )
    assert.equal(second(), true)
  })
})

describe('paymentErrorMessage', () => {
  test('shows the reason, not the word error', () => {
    assert.equal(
      paymentErrorMessage({
        message: 'error',
        data: '充值优惠有更新，请重新确认金额',
      }),
      '充值优惠有更新，请重新确认金额'
    )
    assert.equal(paymentErrorMessage({ message: '无权限' }), '无权限')
    assert.equal(paymentErrorMessage({ message: 'error', data: {} }), 'error')
    assert.equal(paymentErrorMessage({}), undefined)
  })
})
