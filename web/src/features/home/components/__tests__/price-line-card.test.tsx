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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
  type CurrencyConfig,
} from '@/stores/system-config-store'

import { buildPriceLines, type PriceLine } from '../../lib/price-table.ts'

const dom = new Window()
const { PriceLineCard } = await import('../price-line-card')
const i18n = createInstance()
await i18n
  .use(initReactI18next)
  .init({ lng: 'en', resources: { en: { translation: {} } } })
const initialConfig = useSystemConfigStore.getState().config

after(() => {
  useSystemConfigStore.setState({ config: initialConfig })
  dom.close()
})

// 线上 /api/pricing 的真实形态（2026-10-06 抓取后裁剪）。
const PAYLOAD = {
  success: true,
  group_ratio: { 满血稳定codex: 0.4, 'claude kiro': 0.4, 满血稳定claude: 1.75 },
  usable_group: {
    满血稳定codex: '普通线路pro（0.6折）',
    'claude kiro': '普通线路claude（0.6折）',
    满血稳定claude: '满血稳定claude（2.5折）',
  },
  data: [
    {
      model_name: 'claude-opus-5-5',
      quota_type: 0,
      model_ratio: 2,
      completion_ratio: 5,
      enable_groups: ['claude kiro', '满血稳定claude'],
    },
    {
      model_name: 'gpt-6-sol',
      quota_type: 0,
      model_ratio: 1,
      completion_ratio: 5,
      enable_groups: ['满血稳定codex'],
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 272000 ? tier("standard", p * 2 + c * 10 + cr * 0.2 + cc * 2.5) : tier("long_context", p * 4 + c * 15 + cr * 0.4 + cc * 5)',
    },
    {
      model_name: 'gpt-6-luna',
      quota_type: 0,
      model_ratio: 0.05,
      completion_ratio: 5,
      enable_groups: ['满血稳定codex'],
      billing_mode: 'tiered_expr',
      billing_expr: 'p * 0.1 + c * 0.5',
    },
  ],
}

const LINES = buildPriceLines(PAYLOAD, {
  headlineModels: ['claude-opus-5-5', 'gpt-6-sol', 'gpt-6-luna'],
  lineOrder: ['满血稳定claude', 'claude kiro', '满血稳定codex'],
})

// 生产配置：按人民币展示，1 美元额度 = 1 元。
const PRODUCTION: CurrencyConfig = {
  ...DEFAULT_CURRENCY_CONFIG,
  quotaDisplayType: 'CNY',
  usdExchangeRate: 1,
}

function lineOf(group: string): PriceLine {
  const line = LINES.find((item) => item.group === group)
  assert.ok(line, `缺少线路：${group}`)
  return line
}

function renderCard(group: string, currency: CurrencyConfig = PRODUCTION) {
  useSystemConfigStore.setState({ config: { ...initialConfig, currency } })
  const element = dom.document.createElement('div')
  element.innerHTML = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <PriceLineCard line={lineOf(group)} currency={currency} />
    </I18nextProvider>
  )
  const cells = [...element.querySelectorAll('tbody tr')].map((row) =>
    [...row.children].map((cell) => (cell.textContent ?? '').trim())
  )
  return { text: element.textContent ?? '', cells }
}

describe('PriceLineCard', () => {
  test('主推线路：官网价用美元，本站价用人民币，折扣按 1.75 ÷ 7 写成 25%', () => {
    const { text, cells } = renderCard('满血稳定claude')
    assert.deepEqual(cells, [['claude-opus-5-5', '$4 / $20', '¥7 / ¥35']])
    assert.ok(text.includes('About 25% of the official price'), text)
    assert.ok(text.includes('Multiplier 1.75'), text)
    assert.ok(text.includes('Main line'), text)
    assert.ok(text.startsWith('满血稳定claude'), text)
  })

  test('普通线路：同一模型按 0.40 倍率报价，不带主推标记', () => {
    const { text, cells } = renderCard('claude kiro')
    assert.deepEqual(cells, [['claude-opus-5-5', '$4 / $20', '¥1.6 / ¥8']])
    assert.ok(text.includes('About 5.7% of the official price'), text)
    assert.ok(text.includes('Multiplier 0.40'), text)
    assert.ok(!text.includes('Main line'), text)
  })

  test('分档模型报标准档并带星号；看不懂的分档不给价格', () => {
    const { cells } = renderCard('满血稳定codex')
    assert.deepEqual(cells, [
      ['gpt-6-sol *', '$2 / $10', '¥0.8 / ¥4'],
      ['gpt-6-luna', 'See the price table for how this model is priced'],
    ])
  })

  test('站点按美元展示时，本站价也是美元，倍率 ≥ 1 不称折扣', () => {
    const usd: CurrencyConfig = { ...PRODUCTION, quotaDisplayType: 'USD' }
    const stable = renderCard('满血稳定claude', usd)
    assert.deepEqual(stable.cells, [
      ['claude-opus-5-5', '$4 / $20', '$7 / $35'],
    ])
    assert.ok(!stable.text.includes('of the official price'), stable.text)
    assert.ok(stable.text.includes('Multiplier 1.75'), stable.text)

    const normal = renderCard('claude kiro', usd)
    assert.ok(normal.text.includes('About 40% of the official price'))
  })

  test('站点按 tokens 展示时不给折扣，价格仍是货币', () => {
    const tokens: CurrencyConfig = { ...PRODUCTION, quotaDisplayType: 'TOKENS' }
    const { text, cells } = renderCard('claude kiro', tokens)
    assert.ok(!text.includes('of the official price'), text)
    assert.deepEqual(cells, [['claude-opus-5-5', '$4 / $20', '$1.6 / $8']])
  })
})
