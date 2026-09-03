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

const topupInfo: TopupInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  enable_tron_topup: true,
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
        min_topup: 10,
      },
    ])

    await act(async () => root.unmount())
    container.remove()
  })
})
