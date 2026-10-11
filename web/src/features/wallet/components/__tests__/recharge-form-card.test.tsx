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

import type { PaymentMethod, TopupInfo } from '../../types'

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
const { RechargeFormCard } = await import('../recharge-form-card')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en', resources: {} })

// The real Simplified Chinese locale, initialised the way src/i18n/config.ts does.
const zhLocale = (await import('@/i18n/locales/zh.json')).default
const i18nZh = createInstance()
await i18nZh.use(initReactI18next).init({
  lng: 'zhCN',
  fallbackLng: 'en',
  nsSeparator: false,
  resources: { zhCN: zhLocale },
  interpolation: { escapeValue: false },
})

const topupInfo: TopupInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  enable_tron_topup: true,
  tron_min_topup: 100,
  pay_methods: [
    {
      name: 'Legacy TRON',
      type: 'tron',
      color: '#000000',
      min_topup: 999,
    },
    { name: 'Alipay', type: 'alipay', min_topup: 10 },
  ],
  min_topup: 10,
  stripe_min_topup: 10,
  amount_options: [],
  discount: {},
}

describe('RechargeFormCard TRON compatibility', () => {
  after(() => domWindow.close())

  test('renders the dedicated TRON action from its capability flag', async () => {
    const selectedMethods: PaymentMethod[] = []
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <RechargeFormCard
            topupInfo={topupInfo}
            presetAmounts={[]}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={100}
            onTopupAmountChange={() => undefined}
            paymentAmount={100}
            calculating={false}
            onPaymentMethodSelect={(method) => selectedMethods.push(method)}
            paymentLoading={null}
            redemptionCode=''
            onRedemptionCodeChange={() => undefined}
            onRedeem={() => undefined}
            redeeming={false}
          />
        </I18nextProvider>
      )
    })

    const tronButtons = document.querySelectorAll<HTMLButtonElement>(
      'button[aria-label*="TRON"]'
    )
    assert.equal(tronButtons.length, 1)
    assert.equal(
      tronButtons[0]?.getAttribute('aria-label'),
      'USDT (TRON/TRC20)'
    )
    assert.ok(document.querySelector('button[aria-label="Alipay"]'))
    await act(async () => tronButtons[0]?.click())
    assert.deepEqual(selectedMethods, [
      {
        name: 'USDT (TRON/TRC20)',
        type: 'tron',
        color: '#EF0027',
        min_topup: 100,
      },
    ])

    await act(async () => root.unmount())
    container.remove()
  })

  test('disables the TRON action below the TRON-specific minimum', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <RechargeFormCard
            topupInfo={topupInfo}
            presetAmounts={[]}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={50}
            onTopupAmountChange={() => undefined}
            paymentAmount={50}
            calculating={false}
            onPaymentMethodSelect={() => undefined}
            paymentLoading={null}
            redemptionCode=''
            onRedemptionCodeChange={() => undefined}
            onRedeem={() => undefined}
            redeeming={false}
          />
        </I18nextProvider>
      )
    })

    const tronButton = document.querySelector<HTMLButtonElement>(
      'button[aria-label^="USDT (TRON/TRC20)"]'
    )
    assert.ok(tronButton)
    assert.equal(tronButton.disabled, true)
    assert.match(tronButton.getAttribute('aria-label') ?? '', /100/)
    const alipayButton = document.querySelector<HTMLButtonElement>(
      'button[aria-label="Alipay"]'
    )
    assert.ok(alipayButton)
    assert.equal(alipayButton.disabled, false)

    await act(async () => root.unmount())
    container.remove()
  })
})

describe('RechargeFormCard preset discount wording', () => {
  const tiers = [100, 200, 500, 1000, 2000]
  const discount = { 200: 0.98, 500: 0.97, 1000: 0.95, 2000: 0.92 }

  async function presetCardTexts(instance: typeof i18n): Promise<string[]> {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <I18nextProvider i18n={instance}>
          <RechargeFormCard
            topupInfo={{ ...topupInfo, amount_options: tiers, discount }}
            presetAmounts={tiers.map((value) => ({ value }))}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={100}
            onTopupAmountChange={() => undefined}
            paymentAmount={100}
            calculating={false}
            onPaymentMethodSelect={() => undefined}
            paymentLoading={null}
            redemptionCode=''
            onRedemptionCodeChange={() => undefined}
            onRedeem={() => undefined}
            redeeming={false}
          />
        </I18nextProvider>
      )
    })
    // Preset cards are the only buttons carrying the min-h-16 sizing class.
    const texts = [...container.querySelectorAll('button')]
      .filter((button) => button.className.includes('min-h-16'))
      .map((button) =>
        (button.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
      )
    await act(async () => root.unmount())
    container.remove()
    return texts
  }

  test('English keeps the percent-off wording', async () => {
    const texts = await presetCardTexts(i18n)
    assert.equal(texts.length, 5)
    assert.equal(texts[0], '100Pay 100')
    assert.equal(texts[1], '2002% OFFPay 196 • Save 4')
    assert.equal(texts[2], '5003% OFFPay 485 • Save 15')
    assert.match(texts[3] ?? '', /5% OFFPay 950 • Save 50$/)
    assert.match(texts[4] ?? '', /8% OFFPay 1,840 • Save 160$/)
  })

  test('Simplified Chinese says 折 / 实付 / 省 and leaves no English behind', async () => {
    const texts = await presetCardTexts(i18nZh)
    assert.equal(texts.length, 5)
    assert.equal(texts[0], '100实付 100')
    assert.equal(texts[1], '2009.8 折实付 196 • 省 4')
    assert.equal(texts[2], '5009.7 折实付 485 • 省 15')
    assert.match(texts[3] ?? '', /9\.5 折实付 950 • 省 50$/)
    assert.match(texts[4] ?? '', /9\.2 折实付 1,840 • 省 160$/)
    for (const text of texts) {
      assert.doesNotMatch(text, /OFF|Pay|Save/)
    }
  })

  test('a rate too close to 1 keeps the amounts but drops the badge', async () => {
    // 0.9999 would otherwise read "10 折" next to a price that is 0.01 lower
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18nZh}>
          <RechargeFormCard
            topupInfo={{
              ...topupInfo,
              amount_options: [100],
              discount: { 100: 0.9999 },
            }}
            presetAmounts={[{ value: 100 }]}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={100}
            onTopupAmountChange={() => undefined}
            paymentAmount={100}
            calculating={false}
            onPaymentMethodSelect={() => undefined}
            paymentLoading={null}
            redemptionCode=''
            onRedemptionCodeChange={() => undefined}
            onRedeem={() => undefined}
            redeeming={false}
          />
        </I18nextProvider>
      )
    })
    const text = [...container.querySelectorAll('button')]
      .filter((button) => button.className.includes('min-h-16'))
      .map((button) =>
        (button.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
      )
      .join('|')
    assert.equal(text, '100实付 99.99 • 省 0.01')
    await act(async () => root.unmount())
    container.remove()
  })

  test('a tier without a discount shows the price only', async () => {
    for (const instance of [i18n, i18nZh]) {
      const [first] = await presetCardTexts(instance)
      assert.doesNotMatch(first ?? '', /OFF|折|Save|省/)
    }
  })
})
