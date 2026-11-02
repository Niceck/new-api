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

const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { PaymentConfirmDialog } = await import('../payment-confirm-dialog')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const zhLocale = (await import('@/i18n/locales/zh.json')).default
const i18nZh = createInstance()
await i18nZh.use(initReactI18next).init({
  lng: 'zhCN',
  fallbackLng: 'en',
  nsSeparator: false,
  resources: { zhCN: zhLocale },
  interpolation: { escapeValue: false },
})

async function renderDialog(
  props: Partial<Parameters<typeof PaymentConfirmDialog>[0]>
): Promise<{ text: string; bonus: string | null }> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18nZh}>
        <PaymentConfirmDialog
          open
          onOpenChange={() => undefined}
          onConfirm={() => undefined}
          topupAmount={500}
          paymentAmount={500}
          paymentMethod={{ name: '支付宝', type: 'alipay' }}
          calculating={false}
          processing={false}
          {...props}
        />
      </I18nextProvider>
    )
  })
  const flatten = (value: string | null | undefined) =>
    (value ?? '').replaceAll(/\s+/g, ' ').trim()
  const text = flatten(document.body.textContent)
  const bonusNode = document.body.querySelector(
    '[data-testid="payment-confirm-bonus"]'
  )
  const bonus = bonusNode ? flatten(bonusNode.textContent) : null
  await act(async () => root.unmount())
  container.remove()
  return { text, bonus }
}

describe('PaymentConfirmDialog bonus', () => {
  after(async () => {
    await domWindow.happyDOM?.abort?.()
  })

  test('shows the bonus and the total the server quoted', async () => {
    const { bonus, text } = await renderDialog({
      bonusRate: 0.035,
      bonusAmount: 17.5,
      creditAmount: 517.5,
    })
    assert.ok(bonus)
    assert.match(bonus, /赠送额度/)
    assert.match(bonus, /\+\D?17\.5\b/)
    assert.match(bonus, /3\.5%/)
    assert.match(bonus, /实际到账/)
    assert.match(bonus, /517\.5\b/)
    assert.doesNotMatch(text, /您节省/, 'no discount wording next to a bonus')
  })

  test('no bonus row without a bonus', async () => {
    const { bonus } = await renderDialog({})
    assert.equal(bonus, null)
  })

  test('hides the bonus while the quote is being recalculated', async () => {
    const { bonus } = await renderDialog({
      bonusRate: 0.035,
      bonusAmount: 17.5,
      creditAmount: 517.5,
      calculating: true,
    })
    assert.equal(bonus, null)
  })

  test('a discount quote still renders as before', async () => {
    const { bonus, text } = await renderDialog({
      paymentAmount: 485,
      discountRate: 0.97,
    })
    assert.equal(bonus, null)
    assert.match(text, /您节省/)
  })
})
