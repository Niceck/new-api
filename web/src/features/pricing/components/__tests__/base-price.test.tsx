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
import { after, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
  type CurrencyConfig,
} from '@/stores/system-config-store'

import type { PricingModel } from '../../types'
import type { ModelDetailsContentProps } from '../model-details'

const dom = new Window()
const globals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'MutationObserver',
  'getComputedStyle',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'matchMedia',
  'customElements',
  'Document',
  'ShadowRoot',
  'Event',
  'CustomEvent',
] as const
const originalDescriptors = new Map(
  globals.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
)
for (const key of globals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: key === 'matchMedia' ? dom.matchMedia.bind(dom) : dom[key],
  })
}
const { ModelDetailsContent } = await import('../model-details')
const { DynamicPricingBreakdown } = await import('../dynamic-pricing-breakdown')
const i18n = createInstance()
await i18n
  .use(initReactI18next)
  .init({ lng: 'en', resources: { en: { translation: {} } } })
const client = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
const initialConfig = useSystemConfigStore.getState().config

after(() => {
  useSystemConfigStore.setState({ config: initialConfig })
  client.clear()
  dom.close()
  for (const key of globals) {
    const descriptor = originalDescriptors.get(key)
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})

const opus: PricingModel = {
  id: 1,
  model_name: 'claude-opus-5-5',
  quota_type: 0,
  model_ratio: 2,
  completion_ratio: 5,
  cache_ratio: 0.05,
  create_cache_ratio: 1.25,
  enable_groups: ['HQ'],
}
const astra: PricingModel = {
  ...opus,
  model_name: 'gpt-6-astra',
  model_ratio: 5,
  cache_ratio: 0.1,
  enable_groups: ['PRO'],
  billing_mode: 'tiered_expr',
  billing_expr:
    'len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25)',
}

function renderPrices(
  model: PricingModel,
  overrides: Partial<ModelDetailsContentProps> = {},
  currency: Partial<CurrencyConfig> = {}
) {
  const config = {
    ...DEFAULT_CURRENCY_CONFIG,
    quotaDisplayType: 'CNY' as const,
    ...currency,
  }
  useSystemConfigStore.setState({
    config: { ...initialConfig, currency: config },
  })
  const element = dom.document.createElement('div')
  element.innerHTML = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <ModelDetailsContent
          model={model}
          groupRatio={{ HQ: 1.75, PRO: 0.4 }}
          usableGroup={{
            HQ: { desc: 'HQ', ratio: 1.75 },
            PRO: { desc: 'PRO', ratio: 0.4 },
          }}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={1}
          usdExchangeRate={config.usdExchangeRate}
          tokenUnit='M'
          {...overrides}
        />
      </I18nextProvider>
    </QueryClientProvider>
  )
  const headings = [...element.querySelectorAll('h2')]
  const base = headings
    .find((h) => h.textContent?.startsWith('Base Price'))
    ?.closest('section')
  const group = headings
    .find((h) => h.textContent === 'Pricing by Group')
    ?.closest('section')
  assert.ok(base, 'base price section is visible')
  assert.ok(group, 'group price section is visible')
  return {
    element,
    base: base.textContent ?? '',
    group: group.textContent ?? '',
  }
}

function dollarAmounts(text: string): number[] {
  return [...text.matchAll(/[$¥]([\d,.]+)/g)].map((match) =>
    Number(match[1].replaceAll(',', ''))
  )
}

test('Claude base quotes use USD while HQ prices keep CNY and the 1.75 ratio', () => {
  const { base, group } = renderPrices(opus)
  assert.deepEqual(dollarAmounts(base), [4, 20, 0.2, 5])
  assert.ok(base.includes('$4'), base)
  assert.ok(!base.includes('¥'), base)
  assert.deepEqual(dollarAmounts(group), [7, 35, 0.35, 8.75])
  assert.ok(group.includes('¥7'), group)
})

test('GPT tiers use USD base prices while both PRO tiers retain CNY charges', () => {
  const { element, base, group } = renderPrices(astra)
  assert.deepEqual(dollarAmounts(base), [10, 50, 1, 12.5])
  assert.ok(base.includes('$10'), base)
  const tierTable = element.querySelector('table')
  assert.ok(tierTable)
  assert.deepEqual(
    dollarAmounts(tierTable.textContent ?? ''),
    [10, 50, 1, 12.5, 20, 75, 2, 25]
  )
  assert.ok(tierTable.textContent?.includes('$20'))
  assert.ok(!tierTable.textContent?.includes('¥'))
  assert.deepEqual(dollarAmounts(group), [4, 20, 0.4, 5, 8, 30, 0.8, 10])
  assert.ok(group.includes('¥4'), group)
})

test('base quotes ignore recharge discounts and exchange rates, including the 1K unit', () => {
  const { base, group } = renderPrices(
    opus,
    { tokenUnit: 'K', showRechargePrice: true, priceRate: 4 },
    { usdExchangeRate: 7 }
  )
  for (const amount of ['$0.004', '$0.02', '$0.0002', '$0.005']) {
    assert.ok(base.includes(amount), base)
  }
  assert.ok(group.includes('¥0.028'), group)
})

test('fixed per-request base quotes also remain USD', () => {
  const { base, group } = renderPrices({
    ...opus,
    quota_type: 1,
    model_price: 0.5,
  })
  assert.ok(base.includes('$0.5'), base)
  assert.ok(group.includes('¥0.875'), group)
})

test('base quotes remain USD under token and custom wallet display settings', () => {
  for (const quotaDisplayType of ['TOKENS', 'CUSTOM'] as const) {
    const { base } = renderPrices(
      opus,
      {},
      {
        quotaDisplayType,
        customCurrencySymbol: '€',
        customCurrencyExchangeRate: 0.9,
      }
    )
    assert.ok(base.includes('$4'), base)
    assert.ok(base.includes('$20'), base)
  }
})

test('usage-log tier breakdown keeps configured currency when base display is not requested', async () => {
  useSystemConfigStore.setState({
    config: {
      ...initialConfig,
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'CNY',
        usdExchangeRate: 7,
      },
    },
  })
  // Client rendering reads Zustand's current state instead of its SSR defaults.
  const testGlobals = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = testGlobals.IS_REACT_ACT_ENVIRONMENT
  testGlobals.IS_REACT_ACT_ENVIRONMENT = true
  const { act } = await import('react')
  const { createRoot } = await import('react-dom/client')
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <DynamicPricingBreakdown billingExpr={astra.billing_expr} compact />
        </I18nextProvider>
      )
    )
    const text = container.textContent ?? ''
    assert.ok(text.includes('¥70.0000'), text)
    assert.ok(!text.includes('$10.0000'), text)
  } finally {
    await act(async () => root.unmount())
    container.remove()
    testGlobals.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})
