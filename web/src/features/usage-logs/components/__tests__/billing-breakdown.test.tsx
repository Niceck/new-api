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
] as const

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

const { BillingBreakdown } = await import('../dialogs/details-dialog')
const { formatBillingCurrencyFromUSD } = await import('@/lib/currency')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
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
  after(() => {
    domWindow.close()
  })

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
