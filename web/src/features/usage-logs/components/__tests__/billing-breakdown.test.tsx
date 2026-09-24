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
import type React from 'react'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
  'Document',
  'ShadowRoot',
  'customElements',
] as const

const originalDescriptors = new Map(
  domGlobals.map((key) => [
    key,
    Object.getOwnPropertyDescriptor(globalThis, key),
  ])
)
for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Billing Details': 'Billing Details',
        'Billing Mode': 'Billing Mode',
        'Per-token': 'Per-token',
        'Per-call': 'Per-call',
        Input: 'Input',
        Output: 'Output',
        'Model Price': 'Model Price',
        'Group Ratio (included)': 'Group Ratio (included)',
        'Total Cost': 'Total Cost',
      },
    },
  },
})

const { DynamicPricingBreakdown } =
  await import('@/features/pricing/components/dynamic-pricing-breakdown')
const { useSystemConfigStore, DEFAULT_CURRENCY_CONFIG } =
  await import('@/stores/system-config-store')
const { BillingBreakdown } = await import('../dialogs/details-dialog')
const { formatBillingCurrencyFromUSD } = await import('@/lib/currency')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
const originalActEnvironment = reactTestGlobals.IS_REACT_ACT_ENVIRONMENT
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const PRICE_OPTS = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }

type Rendered = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderBreakdown(
  props: React.ComponentProps<typeof BillingBreakdown>
): Promise<Rendered> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <BillingBreakdown {...props} />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmount(rendered: Rendered) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

function text(rendered: Rendered): string {
  return (rendered.container.textContent ?? '').replaceAll(/\s/g, '')
}

function price(usd: number): string {
  return formatBillingCurrencyFromUSD(usd, PRICE_OPTS).replaceAll(/\s/g, '')
}

describe('billing breakdown prices', () => {
  // The HQ line runs at a group ratio above 1, so a missing multiplier is
  // visible as an under-reported price rather than a rounding difference.
  test('multiplies per-token prices by the group ratio actually charged', async () => {
    const rendered = await renderBreakdown({
      log: { quota: 38893 } as React.ComponentProps<
        typeof BillingBreakdown
      >['log'],
      other: {
        model_ratio: 2.5714,
        completion_ratio: 5,
        group_ratio: 1.75,
      },
      isAdmin: false,
    })

    const content = text(rendered)
    assert.equal(content.includes(price(8.9999)), true)
    assert.equal(content.includes(price(44.9995)), true)
    assert.equal(content.includes(price(5.1428)), false)

    await unmount(rendered)
  })

  test('multiplies per-token prices by a user-exclusive ratio when present', async () => {
    const rendered = await renderBreakdown({
      log: { quota: 1000 } as React.ComponentProps<
        typeof BillingBreakdown
      >['log'],
      other: {
        model_ratio: 2.5714,
        completion_ratio: 5,
        group_ratio: 1.75,
        user_group_ratio: 0.5,
      },
      isAdmin: false,
    })

    assert.equal(text(rendered).includes(price(2.5714)), true)

    await unmount(rendered)
  })

  test('multiplies the per-call price by the group ratio actually charged', async () => {
    const rendered = await renderBreakdown({
      log: { quota: 1000 } as React.ComponentProps<
        typeof BillingBreakdown
      >['log'],
      other: {
        model_price: 0.1,
        group_ratio: 1.75,
      },
      isAdmin: false,
    })

    assert.equal(text(rendered).includes(price(0.175)), true)

    await unmount(rendered)
  })

  test('leaves prices untouched when the log carries no ratio', async () => {
    const rendered = await renderBreakdown({
      log: { quota: 1000 } as React.ComponentProps<
        typeof BillingBreakdown
      >['log'],
      other: {
        model_ratio: 2.5714,
        completion_ratio: 5,
      },
      isAdmin: false,
    })

    assert.equal(text(rendered).includes(price(5.1428)), true)

    await unmount(rendered)
  })
})

test('non-Claude ratio logs with cache usage show the charged cache read price', async () => {
  const rendered = await renderBreakdown({
    log: { quota: 400 } as React.ComponentProps<typeof BillingBreakdown>['log'],
    other: {
      model_ratio: 1,
      completion_ratio: 5,
      group_ratio: 0.4,
      cache_tokens: 1000,
      cache_ratio: 0.1,
    },
    isAdmin: false,
  })
  try {
    assert.ok(text(rendered).includes(`CacheRead${price(0.08)}/M`))
  } finally {
    await unmount(rendered)
  }
})

for (const [name, input, output] of [
  ['astra', 10, 50],
  ['sol', 2, 10],
  ['luna', 0.1, 0.5],
] as const) {
  for (const [tier, pin, pout] of [
    ['standard', input, output],
    ['long_context', input * 2, output * 1.5],
  ] as const) {
    test(`${name} ${tier} billing details use the expression snapshot instead of legacy ratios`, async () => {
      const expr = `len <= 272000 ? tier("standard", p * ${input} + c * ${output}) : tier("long_context", p * ${input * 2} + c * ${output * 1.5})`
      const rendered = await renderBreakdown({
        log: { quota: 400 } as React.ComponentProps<
          typeof BillingBreakdown
        >['log'],
        other: {
          model_ratio: 1,
          completion_ratio: 5,
          group_ratio: 0.4,
          billing_mode: 'tiered_expr',
          expr_b64: btoa(expr),
          matched_tier: tier,
        },
        isAdmin: false,
      })
      try {
        assert.ok(text(rendered).includes(`Input${price(pin * 0.4)}/M`))
        assert.ok(text(rendered).includes(`Output${price(pout * 0.4)}/M`))
      } finally {
        await unmount(rendered)
      }
    })
  }
}

for (const ratio of [0.4, 1.75, 0]) {
  test(`usage-log tier table applies settled multiplier ${ratio} to desktop and mobile prices`, async () => {
    const initialConfig = useSystemConfigStore.getState().config
    useSystemConfigStore.setState({
      config: {
        ...initialConfig,
        currency: {
          ...DEFAULT_CURRENCY_CONFIG,
          quotaDisplayType: 'CNY',
          usdExchangeRate: 1,
        },
      },
    })
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    try {
      await act(async () =>
        root.render(
          <I18nextProvider i18n={i18n}>
            <DynamicPricingBreakdown
              compact
              groupRatio={ratio}
              billingExpr='tier("standard", p * 10 + c * 50 + cr * 1)'
            />
          </I18nextProvider>
        )
      )
      const amounts = [...container.querySelectorAll('span, div')]
        .filter((node) => node.children.length === 0)
        .map((node) => node.textContent)
      for (const base of [10, 50, 1]) {
        assert.equal(
          amounts.filter((value) => value === `¥${(base * ratio).toFixed(4)}`)
            .length,
          ratio === 0 ? 6 : 2
        )
      }
    } finally {
      await act(async () => root.unmount())
      container.remove()
      useSystemConfigStore.setState({ config: initialConfig })
    }
  })
}

after(() => {
  domWindow.close()
  reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  for (const key of domGlobals) {
    const descriptor = originalDescriptors.get(key)
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})

test('structured tool surcharges show per-call price and subtotal using the settled group ratio', async () => {
  const rendered = await renderBreakdown({
    log: { quota: 17500 } as React.ComponentProps<
      typeof BillingBreakdown
    >['log'],
    other: {
      model_ratio: 5,
      completion_ratio: 5,
      group_ratio: 1.75,
      tool_surcharges: [{ name: 'web_search', count: 2, price: 10 }],
    },
    isAdmin: false,
  })
  try {
    const content = text(rendered)
    assert.ok(content.includes('web_search'))
    assert.ok(content.includes(`2×${price(0.0175)}=${price(0.035)}`), content)
  } finally {
    await unmount(rendered)
  }
})
