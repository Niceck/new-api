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

import type { TronTopupOrder } from '../../../types'

const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLFormElement',
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

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    domWindow.HTMLInputElement.prototype,
    'value'
  )?.set
  setter?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TronPaymentDialog } = await import('../tron-payment-dialog')
const { formatTronCNYValue, formatTronUSDTAmount } =
  await import('../../../lib/tron')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: { 'Price data by CoinGecko': 'Price data by CoinGecko' },
    },
    zh: {
      translation: { 'Price data by CoinGecko': '汇率数据由 CoinGecko 提供' },
    },
  },
})

const order: TronTopupOrder = {
  trade_no: 'TRON-order-1',
  network: 'mainnet',
  receive_address: 'TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f',
  token_contract: 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t',
  expected_usdt_micros: 13_791_234,
  rate_cny_micros: 7_250_000,
  quote_updated_at_ms: Date.now(),
  expires_at_ms: Date.now() + 20 * 60_000,
  credit_quota: 50_000_000,
  status: 'pending',
}

async function renderDialog(
  props: Partial<React.ComponentProps<typeof TronPaymentDialog>> = {}
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <TronPaymentDialog
          open
          order={order}
          onOpenChange={() => undefined}
          onSubmitClaim={async () => true}
          claiming={false}
          {...props}
        />
      </I18nextProvider>
    )
  })
  return { container, root }
}

describe('TronPaymentDialog', () => {
  after(() => domWindow.close())

  test('shows exact payment evidence with accessible copy controls', async () => {
    const copied: string[] = []
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (value: string) => copied.push(value) },
    })
    const rendered = await renderDialog()
    const bodyText = document.body.textContent ?? ''
    assert.equal(formatTronUSDTAmount(13_791_234), '13.791234')
    assert.equal(bodyText.includes('13.791234 USDT'), true)
    assert.equal(bodyText.includes(order.receive_address), true)
    assert.equal(bodyText.includes(order.token_contract), true)
    assert.equal(formatTronCNYValue(13_791_234, 7_250_000), '99.99')
    assert.equal(bodyText.includes('¥99.99'), true)
    assert.equal(bodyText.includes('TRON / TRC20'), true)
    assert.equal(
      bodyText.includes(
        'Exchange withdrawals may deduct a network fee from the amount sent.'
      ),
      true
    )
    const priceAttribution = document.querySelector<HTMLAnchorElement>(
      'a[href="https://www.coingecko.com/en/api"]'
    )
    assert.ok(priceAttribution)
    assert.equal(
      priceAttribution.textContent?.trim(),
      'Price data by CoinGecko'
    )
    assert.equal(priceAttribution.target, '_blank')
    assert.match(priceAttribution.rel, /noopener/)
    assert.match(priceAttribution.rel, /noreferrer/)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    assert.equal(
      priceAttribution.textContent?.trim(),
      '汇率数据由 CoinGecko 提供'
    )
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    assert.ok(
      document.querySelector('svg[aria-label="TRON payment address QR code"]')
    )
    assert.ok(
      document.querySelector('button[aria-label="Copy payment amount"]')
    )
    assert.ok(
      document.querySelector('button[aria-label="Copy receiving address"]')
    )
    assert.ok(document.querySelector('[aria-live="polite"]'))
    assert.equal(
      document.querySelector('[aria-live="polite"]')?.textContent,
      ''
    )
    assert.ok(document.querySelector('[data-tron-address="true"].break-all'))

    const copyAddress = document.querySelector<HTMLButtonElement>(
      'button[aria-label="Copy receiving address"]'
    )
    assert.ok(copyAddress)
    await act(async () => copyAddress.click())
    assert.deepEqual(copied, [order.receive_address])

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  test('shows a labeled claim form for an expired order', async () => {
    const expiredOrder: TronTopupOrder = {
      ...order,
      status: 'expired',
      expires_at_ms: Date.now() - 1,
    }
    const rendered = await renderDialog({ order: expiredOrder })
    assert.ok(document.querySelector('label[for="tron-claim-txid"]'))
    assert.ok(document.querySelector('#tron-claim-txid[inputmode="text"]'))
    assert.ok(document.querySelector('button[type="submit"]'))
    assert.ok(document.querySelector('[role="alert"]'))

    const txInput = document.querySelector<HTMLInputElement>('#tron-claim-txid')
    assert.ok(txInput)
    await act(async () => {
      setInputValue(txInput, 'g'.repeat(64))
    })
    assert.equal(txInput.getAttribute('aria-invalid'), 'true')

    await act(async () => {
      rendered.root.render(
        <I18nextProvider i18n={i18n}>
          <TronPaymentDialog
            open={false}
            order={expiredOrder}
            onOpenChange={() => undefined}
            onSubmitClaim={async () => true}
            claiming={false}
          />
        </I18nextProvider>
      )
    })
    await act(async () => {
      rendered.root.render(
        <I18nextProvider i18n={i18n}>
          <TronPaymentDialog
            open
            order={expiredOrder}
            onOpenChange={() => undefined}
            onSubmitClaim={async () => true}
            claiming={false}
          />
        </I18nextProvider>
      )
    })
    assert.equal(
      document.querySelector<HTMLInputElement>('#tron-claim-txid')?.value,
      ''
    )

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  test('submits canonical user-entered claim evidence', async () => {
    const claims: Array<{ txID: string; note: string }> = []
    const rendered = await renderDialog({
      order: { ...order, status: 'expired', expires_at_ms: Date.now() - 1 },
      onSubmitClaim: async (txID, note) => {
        claims.push({ txID, note })
        return true
      },
    })
    const txInput = document.querySelector<HTMLInputElement>('#tron-claim-txid')
    const noteInput =
      document.querySelector<HTMLInputElement>('#tron-claim-note')
    const form = document.querySelector<HTMLFormElement>('form')
    assert.ok(txInput)
    assert.ok(noteInput)
    assert.ok(form)
    await act(async () => {
      setInputValue(txInput, 'A'.repeat(64))
      setInputValue(noteInput, 'exchange fee')
    })
    await act(async () =>
      form.dispatchEvent(
        new Event('submit', { bubbles: true, cancelable: true })
      )
    )
    assert.deepEqual(claims, [{ txID: 'A'.repeat(64), note: 'exchange fee' }])
    assert.equal(
      document.body.textContent?.includes(
        'Your claim was submitted for manual review.'
      ),
      true
    )

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  test('reopens an expired pending order without a stale payable frame', async () => {
    const originalNow = Date.now
    let currentTime = 1_800_000_000_000
    Date.now = () => currentTime
    try {
      const expiringOrder: TronTopupOrder = {
        ...order,
        status: 'pending',
        expires_at_ms: currentTime + 1_000,
      }
      const rendered = await renderDialog({ order: expiringOrder })
      await act(async () => {
        rendered.root.render(
          <I18nextProvider i18n={i18n}>
            <TronPaymentDialog
              open={false}
              order={expiringOrder}
              onOpenChange={() => undefined}
              onSubmitClaim={async () => true}
              claiming={false}
            />
          </I18nextProvider>
        )
      })
      currentTime += 2_000
      await act(async () => {
        rendered.root.render(
          <I18nextProvider i18n={i18n}>
            <TronPaymentDialog
              open
              order={expiringOrder}
              onOpenChange={() => undefined}
              onSubmitClaim={async () => true}
              claiming={false}
            />
          </I18nextProvider>
        )
      })
      assert.equal(document.body.textContent?.includes('Expired'), true)

      await act(async () => rendered.root.unmount())
      rendered.container.remove()
    } finally {
      Date.now = originalNow
    }
  })
  test('expired orders remove payable QR and allow manual refresh', async () => {
    const rendered = await renderDialog({
      order: { ...order, status: 'expired', expires_at_ms: Date.now() - 1000 },
    })
    assert.equal(
      document.querySelector('svg[aria-label="TRON payment address QR code"]'),
      null
    )
    assert.ok(
      document.querySelector('button[aria-label="Refresh payment status"]')
    )
    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })
  test('credited expired order announces credit rather than waiting for payment', async () => {
    const rendered = await renderDialog({
      order: { ...order, status: 'success', expires_at_ms: Date.now() - 1000 },
    })
    assert.equal(
      document.body.textContent?.includes(
        'Payment deadline passed. You can still check for credit.'
      ),
      false
    )
    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  const tolerantOrder: TronTopupOrder = {
    ...order,
    tolerant_match: true,
    tolerant_min_usdt_micros: 12_412_111,
    tolerant_max_usdt_micros: 15_170_357,
  }
  const newWording = [
    'Send at least the amount due before the countdown ends',
    'Amount due',
    'An amount that is not exact but between {{min}} and {{max}} USDT is credited automatically only if it arrives at least 60 seconds after this order was created, before the countdown ends, and matches this order alone',
    'Paying more adds no credit and is not refunded',
    'Paying less is credited in proportion and settles this order — create a new order to top up the difference',
    'Anything else is reviewed manually, as are orders created after several others within 24 hours — submit a claim with the transaction ID',
    'Amount received',
    'Amount credited',
    'Paid less than the amount due, so it was credited in proportion and this order is settled',
    'Paid more than the amount due, so the face value of the order was credited',
    'Credited {{credited}} for {{received}} USDT received',
  ]

  async function bodyTextFor(shown: TronTopupOrder): Promise<string> {
    const rendered = await renderDialog({ order: shown })
    const bodyText = document.body.textContent ?? ''
    await act(async () => rendered.root.unmount())
    rendered.container.remove()
    return bodyText
  }

  test('states the automatic range and the over and under rules when tolerant matching is on', async () => {
    const bodyText = await bodyTextFor(tolerantOrder)
    for (const expected of [
      'Send at least the amount due before the countdown ends',
      'Amount due',
      'An amount that is not exact but between 12.412111 and 15.170357 USDT is credited automatically only if it arrives at least 60 seconds after this order was created, before the countdown ends, and matches this order alone',
      'Paying more adds no credit and is not refunded',
      'Paying less is credited in proportion and settles this order',
      'as are orders created after several others within 24 hours — submit a claim with the transaction ID',
    ]) {
      assert.equal(bodyText.includes(expected), true, expected)
    }
    assert.equal(
      bodyText.includes('Send the exact amount before the countdown ends.'),
      false
    )
    assert.equal(
      bodyText.includes(
        'Make sure the amount that arrives equals the exact amount.'
      ),
      false
    )
  })

  test('keeps the exact-amount wording when tolerant matching is off', async () => {
    const bodyText = await bodyTextFor(order)
    assert.equal(
      bodyText.includes('Send the exact amount before the countdown ends.'),
      true
    )
    assert.equal(bodyText.includes('Exact amount'), true)
    assert.equal(
      bodyText.includes('Paying more adds no credit and is not refunded'),
      false
    )
    assert.equal(bodyText.includes('is credited automatically'), false)
  })

  test('shows what arrived and what was credited for an underpaid order', async () => {
    const bodyText = await bodyTextFor({
      ...tolerantOrder,
      status: 'success',
      received_usdt_micros: 12_900_000,
      credited_quota: 46_769_000,
      match_mode: 'tolerant',
    })
    assert.equal(bodyText.includes('Amount received'), true)
    assert.equal(bodyText.includes('12.900000 USDT'), true)
    assert.equal(bodyText.includes('Amount credited'), true)
    assert.equal(
      bodyText.includes(
        'Paid less than the amount due, so it was credited in proportion and this order is settled'
      ),
      true
    )
    assert.equal(bodyText.includes('Paid more than the amount due'), false)
  })

  test('explains that an overpaid order was credited at face value', async () => {
    const bodyText = await bodyTextFor({
      ...tolerantOrder,
      status: 'success',
      received_usdt_micros: 14_000_000,
      credited_quota: 50_000_000,
      match_mode: 'tolerant',
    })
    assert.equal(bodyText.includes('14.000000 USDT'), true)
    assert.equal(
      bodyText.includes(
        'Paid more than the amount due, so the face value of the order was credited'
      ),
      true
    )
    assert.equal(bodyText.includes('Paid less than the amount due'), false)
  })

  test('an exactly paid order needs no settlement note', async () => {
    const bodyText = await bodyTextFor({
      ...tolerantOrder,
      status: 'success',
      received_usdt_micros: order.expected_usdt_micros,
      credited_quota: 50_000_000,
      match_mode: 'exact',
    })
    assert.equal(bodyText.includes('Amount received'), false)
    assert.equal(bodyText.includes('Paid less than the amount due'), false)
    assert.equal(bodyText.includes('Paid more than the amount due'), false)
  })

  test('an order credited above its face value before the cap shows facts without a rule', async () => {
    // v30 的人工通过会按比例多给：这类历史订单不能被解释成「按面值入账」。
    const bodyText = await bodyTextFor({
      ...tolerantOrder,
      status: 'success',
      received_usdt_micros: 15_170_000,
      credited_quota: 55_000_000,
      match_mode: '',
    })
    assert.equal(bodyText.includes('Amount received'), true)
    assert.equal(bodyText.includes('Amount credited'), true)
    assert.equal(bodyText.includes('Paid more than the amount due'), false)
    assert.equal(bodyText.includes('Paid less than the amount due'), false)
  })

  test('every locale carries the new wording under keys without periods', async () => {
    for (const locale of ['en', 'zh', 'zh-TW', 'ja', 'fr', 'ru', 'vi']) {
      const messages = (await import(`@/i18n/locales/${locale}.json`)).default
        .translation as Record<string, string>
      for (const key of newWording) {
        assert.equal(key.includes('.'), false, key)
        assert.equal(typeof messages[key], 'string', `${locale}: ${key}`)
        assert.notEqual(messages[key].trim(), '', `${locale}: ${key}`)
        if (locale === 'en') {
          assert.equal(messages[key], key)
        } else {
          assert.notEqual(messages[key], key, `${locale}: ${key}`)
        }
        for (const placeholder of key.match(/{{\w+}}/g) ?? []) {
          assert.equal(
            messages[key].includes(placeholder),
            true,
            `${locale}: ${key} keeps ${placeholder}`
          )
        }
      }
    }
  })
})
