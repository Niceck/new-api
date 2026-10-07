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

import {
  confirmCurrency,
  isAwaitingCurrency,
  resolveHomePricing,
} from '../home-pricing-state.ts'
import type { PriceLine } from '../price-table.ts'

const CNY = {
  displayInCurrency: true,
  quotaDisplayType: 'CNY' as const,
  quotaPerUnit: 500000,
  usdExchangeRate: 1,
  customCurrencySymbol: '¤',
  customCurrencyExchangeRate: 1,
}
// 站点配置仓库的出厂默认值：状态接口返回之前就是它。
const DEFAULT_USD = { ...CNY, quotaDisplayType: 'USD' as const }

const LINES: PriceLine[] = [
  {
    group: '满血稳定claude',
    label: '满血稳定claude',
    ratio: 1.75,
    rows: [],
  },
]

describe('confirmCurrency', () => {
  test('状态接口还没有数据时，仓库里的默认币种不算数', () => {
    assert.equal(confirmCurrency(null, DEFAULT_USD), null)
  })

  test('仓库与状态接口说法一致时才采信，返回仓库里的那份', () => {
    assert.equal(confirmCurrency({ ...CNY }, CNY), CNY)
  })

  test('展示币种、汇率、自定义符号或自定义汇率任一不同都不采信', () => {
    assert.equal(confirmCurrency(CNY, DEFAULT_USD), null)
    assert.equal(confirmCurrency(CNY, { ...CNY, usdExchangeRate: 7 }), null)
    assert.equal(
      confirmCurrency(CNY, { ...CNY, customCurrencySymbol: '€' }),
      null
    )
    assert.equal(
      confirmCurrency(CNY, { ...CNY, customCurrencyExchangeRate: 2 }),
      null
    )
  })
})

describe('resolveHomePricing', () => {
  test('价格先到、币种还没确认：继续显示加载，不放出任何数字', () => {
    const state = resolveHomePricing({
      lines: LINES,
      pricingLoading: false,
      currency: null,
      statusFetching: true,
      currencyWaitExpired: false,
    })
    assert.deepEqual(state, {
      lines: [],
      currency: null,
      isLoading: true,
      isUnavailable: false,
    })
  })

  test('币种先到、价格还在路上：加载中', () => {
    const state = resolveHomePricing({
      lines: [],
      pricingLoading: true,
      currency: CNY,
      statusFetching: false,
      currencyWaitExpired: false,
    })
    assert.equal(state.isLoading, true)
    assert.equal(state.isUnavailable, false)
    assert.deepEqual(state.lines, [])
  })

  test('价格与币种都到位：给出线路和币种', () => {
    const state = resolveHomePricing({
      lines: LINES,
      pricingLoading: false,
      currency: CNY,
      statusFetching: false,
      currencyWaitExpired: false,
    })
    assert.deepEqual(state, {
      lines: LINES,
      currency: CNY,
      isLoading: false,
      isUnavailable: false,
    })
  })

  test('状态接口失败、币种始终无法确认：降级，而不是用默认币种标价', () => {
    const state = resolveHomePricing({
      lines: LINES,
      pricingLoading: false,
      currency: null,
      statusFetching: false,
      currencyWaitExpired: false,
    })
    assert.deepEqual(state, {
      lines: [],
      currency: null,
      isLoading: false,
      isUnavailable: true,
    })
  })

  test('价格接口没给出可报价的线路：降级', () => {
    const state = resolveHomePricing({
      lines: [],
      pricingLoading: false,
      currency: CNY,
      statusFetching: false,
      currencyWaitExpired: false,
    })
    assert.equal(state.isUnavailable, true)
    assert.equal(state.currency, null)
  })

  test('状态请求一直不结束：等待到期后降级，不再停在加载', () => {
    const state = resolveHomePricing({
      lines: LINES,
      pricingLoading: false,
      currency: null,
      statusFetching: true,
      currencyWaitExpired: true,
    })
    assert.deepEqual(state, {
      lines: [],
      currency: null,
      isLoading: false,
      isUnavailable: true,
    })
  })

  test('价格已经失败、状态请求还在途：直接降级，不等币种', () => {
    const state = resolveHomePricing({
      lines: [],
      pricingLoading: false,
      currency: null,
      statusFetching: true,
      currencyWaitExpired: false,
    })
    assert.equal(state.isLoading, false)
    assert.equal(state.isUnavailable, true)
  })

  test('到期降级之后状态请求成功：恢复报价', () => {
    const state = resolveHomePricing({
      lines: LINES,
      pricingLoading: false,
      currency: CNY,
      statusFetching: false,
      currencyWaitExpired: true,
    })
    assert.deepEqual(state, {
      lines: LINES,
      currency: CNY,
      isLoading: false,
      isUnavailable: false,
    })
  })
})

describe('isAwaitingCurrency', () => {
  const waiting = {
    lines: LINES,
    pricingLoading: false,
    currency: null,
    statusFetching: true,
  }

  test('只有「价格到了、有线路、币种没确认、状态请求在途」才算在等币种', () => {
    assert.equal(isAwaitingCurrency(waiting), true)
    assert.equal(
      isAwaitingCurrency({ ...waiting, pricingLoading: true }),
      false
    )
    assert.equal(isAwaitingCurrency({ ...waiting, lines: [] }), false)
    assert.equal(isAwaitingCurrency({ ...waiting, currency: CNY }), false)
    assert.equal(
      isAwaitingCurrency({ ...waiting, statusFetching: false }),
      false
    )
  })
})
