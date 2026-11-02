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

import type { PaymentMethod, TopupInfo, TopupTierStatus } from '../../types'

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

describe('RechargeFormCard member tier', () => {
  const tiers = [100, 200, 500, 1000, 2000]
  const discount = { 200: 0.98, 500: 0.97, 1000: 0.95, 2000: 0.92 }
  const ladder = [
    { level: 1, threshold: 1000, discount: 0.98 },
    { level: 2, threshold: 2500, discount: 0.97 },
    { level: 3, threshold: 5000, discount: 0.95 },
    { level: 4, threshold: 10000, discount: 0.92 },
  ]
  const vip2: TopupTierStatus = {
    total: 2735,
    level: 2,
    discount: 0.97,
    threshold: 2500,
    next_level: 3,
    next_threshold: 5000,
    next_discount: 0.95,
    remaining: 2265,
    tiers: ladder,
  }

  async function renderWithTier(
    instance: typeof i18n,
    tier: TopupTierStatus | null
  ): Promise<{ banner: string | null; cards: string[] }> {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <I18nextProvider i18n={instance}>
          <RechargeFormCard
            topupInfo={{ ...topupInfo, amount_options: tiers, discount, tier }}
            presetAmounts={tiers.map((value) => ({ value }))}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={100}
            onTopupAmountChange={() => undefined}
            paymentAmount={97}
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
    const bannerNode = container.querySelector('[data-testid="topup-tier"]')
    const banner = bannerNode
      ? (bannerNode.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
      : null
    const cards = [...container.querySelectorAll('button')]
      .filter((button) => button.className.includes('min-h-16'))
      .map((button) =>
        (button.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
      )
    await act(async () => root.unmount())
    container.remove()
    return { banner, cards }
  }

  test('Chinese banner: level, rate, total and the road to the next level', async () => {
    const { banner } = await renderWithTier(i18nZh, vip2)
    assert.ok(banner)
    assert.match(banner, /VIP2/)
    assert.match(banner, /每笔充值 9\.7 折/)
    assert.match(banner, /累计充值 2,735/)
    assert.match(banner, /再充 2,265 升级 VIP3（9\.5 折）/)
    assert.match(banner, /不叠加/)
    assert.doesNotMatch(banner, /OFF|Pay|Save|Top up|Member level/)
  })

  test('preset badges show the better of the single-order and tier rates', async () => {
    const { cards } = await renderWithTier(i18nZh, vip2)
    assert.equal(cards.length, 5)
    assert.equal(cards[0], '1009.7 折实付 97 • 省 3') // no single-order discount: tier applies
    assert.equal(cards[1], '2009.7 折实付 194 • 省 6') // 9.8 → tier 9.7 is better
    assert.equal(cards[2], '5009.7 折实付 485 • 省 15') // equal: unchanged, not 0.97²
    assert.match(cards[3] ?? '', /9\.5 折实付 950 • 省 50$/) // single-order better
    assert.match(cards[4] ?? '', /9\.2 折实付 1,840 • 省 160$/)
  })

  test('below the first level the banner invites and the badges are unchanged', async () => {
    const { banner, cards } = await renderWithTier(i18nZh, {
      total: 300,
      level: 0,
      discount: 1,
      next_level: 1,
      next_threshold: 1000,
      next_discount: 0.98,
      remaining: 700,
      tiers: ladder,
    })
    assert.ok(banner)
    assert.match(banner, /VIP0/)
    assert.match(banner, /累计充值满 1,000 起享永久折扣/)
    assert.match(banner, /累计充值 300/)
    assert.match(banner, /再充 700 升级 VIP1（9\.8 折）/)
    assert.equal(cards[0], '100实付 100')
    assert.equal(cards[1], '2009.8 折实付 196 • 省 4')
  })

  test('the top level says so, in English too', async () => {
    const { banner } = await renderWithTier(i18n, {
      total: 10190,
      level: 4,
      discount: 0.92,
      threshold: 10000,
      tiers: ladder,
    })
    assert.ok(banner)
    assert.match(banner, /VIP4/)
    assert.match(banner, /Every top-up at 8% off/)
    assert.match(banner, /Total topped up 10,190/)
    assert.match(banner, /Highest level reached/)
    assert.doesNotMatch(banner, /more to reach/)
  })

  test('no banner when tiers are not configured', async () => {
    const { banner, cards } = await renderWithTier(i18nZh, null)
    assert.equal(banner, null)
    assert.equal(cards[1], '2009.8 折实付 196 • 省 4')
    assert.equal(
      document.querySelector('[data-testid="topup-tier-unavailable"]'),
      null
    )
  })

  test('says so when the tier could not be read, instead of looking unconfigured', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18nZh}>
          <RechargeFormCard
            topupInfo={{
              ...topupInfo,
              amount_options: tiers,
              discount,
              tier: null,
              tier_unavailable: true,
            }}
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
    const note = container.querySelector(
      '[data-testid="topup-tier-unavailable"]'
    )
    assert.ok(note)
    assert.match(note.textContent ?? '', /会员等级信息暂时读取不到/)
    assert.equal(container.querySelector('[data-testid="topup-tier"]'), null)
    await act(async () => root.unmount())
    container.remove()
  })
})

describe('RechargeFormCard bonus mode', () => {
  const amounts = [100, 200, 500, 1000, 2000]
  const bonus = { 200: 0.025, 500: 0.035, 1000: 0.055, 2000: 0.09 }
  const ladder = [
    { level: 1, threshold: 1000, discount: 1, bonus: 0.025 },
    { level: 2, threshold: 2500, discount: 1, bonus: 0.035 },
    { level: 3, threshold: 5000, discount: 1, bonus: 0.055 },
    { level: 4, threshold: 10000, discount: 1, bonus: 0.09 },
  ]
  const vip2: TopupTierStatus = {
    total: 3000,
    level: 2,
    discount: 1,
    bonus: 0.035,
    threshold: 2500,
    next_level: 3,
    next_threshold: 5000,
    next_discount: 1,
    next_bonus: 0.055,
    remaining: 2000,
    tiers: ladder,
  }
  const newcomer: TopupTierStatus = {
    total: 300,
    level: 0,
    discount: 1,
    next_level: 1,
    next_threshold: 1000,
    next_discount: 1,
    next_bonus: 0.025,
    remaining: 700,
    tiers: ladder,
  }

  async function renderBonus(
    instance: typeof i18n,
    tier: TopupTierStatus | null,
    quote: { rate: number; amount: number; credit: number } = {
      rate: 0,
      amount: 0,
      credit: 0,
    }
  ): Promise<{
    banner: string
    cards: string[]
    quoteLine: string | null
    ranges: string | null
  }> {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <I18nextProvider i18n={instance}>
          <RechargeFormCard
            topupInfo={{
              ...topupInfo,
              amount_options: amounts,
              discount: {},
              bonus,
              tier,
            }}
            presetAmounts={amounts.map((value) => ({ value }))}
            selectedPreset={null}
            onSelectPreset={() => undefined}
            topupAmount={600}
            onTopupAmountChange={() => undefined}
            paymentAmount={600}
            paymentBonusRate={quote.rate}
            paymentBonusAmount={quote.amount}
            paymentCreditAmount={quote.credit}
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
    const text = (node: Element | null) =>
      (node?.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
    const banner = text(container.querySelector('[data-testid="topup-tier"]'))
    const quoteNode = container.querySelector(
      '[data-testid="topup-bonus-quote"]'
    )
    const rangesNode = container.querySelector(
      '[data-testid="topup-bonus-ranges"]'
    )
    const cards = [...container.querySelectorAll('button')]
      .filter((button) => button.className.includes('min-h-16'))
      .map((button) => text(button))
    await act(async () => root.unmount())
    container.remove()
    return {
      banner,
      cards,
      quoteLine: quoteNode ? text(quoteNode) : null,
      ranges: rangesNode ? text(rangesNode) : null,
    }
  }

  test('newcomer: cards show the price and what lands in the wallet', async () => {
    const { cards } = await renderBonus(i18nZh, newcomer)
    assert.equal(cards[0], '100实付 100', 'below the threshold: no badge')
    assert.equal(cards[1], '200送 2.5%实付 200 • 到账 205')
    assert.equal(cards[2], '500送 3.5%实付 500 • 到账 517.5')
    assert.equal(cards[3], '1,000送 5.5%实付 1,000 • 到账 1,055')
    assert.equal(cards[4], '2,000送 9%实付 2,000 • 到账 2,180')
  })

  test('member: the tier lifts small amounts, a higher single-order rate still wins', async () => {
    const { cards } = await renderBonus(i18nZh, vip2)
    assert.equal(cards[0], '100送 3.5%实付 100 • 到账 103.5')
    assert.equal(cards[1], '200送 3.5%实付 200 • 到账 207')
    assert.equal(cards[2], '500送 3.5%实付 500 • 到账 517.5', 'not 7%')
    assert.equal(cards[4], '2,000送 9%实付 2,000 • 到账 2,180')
  })

  test('Chinese banner says bonus, not discount', async () => {
    const { banner } = await renderBonus(i18nZh, vip2)
    assert.match(banner, /VIP2/)
    assert.match(banner, /每笔充值多送 3\.5% 额度/)
    assert.match(banner, /累计充值 3,000/)
    assert.match(banner, /再充 2,000 升级 VIP3（多送 5\.5%）/)
    assert.match(banner, /不叠加/)
    assert.doesNotMatch(banner, /折|OFF|Top up|extra credit/)
  })

  test('below the first level the banner invites with the bonus', async () => {
    const { banner } = await renderBonus(i18nZh, newcomer)
    assert.match(banner, /累计充值满 1,000 起每笔多送额度/)
    assert.match(banner, /再充 700 升级 VIP1（多送 2\.5%）/)
    assert.doesNotMatch(banner, /折/)
  })

  test('the custom amount line uses the figures the server quoted', async () => {
    const none = await renderBonus(i18nZh, newcomer)
    assert.equal(none.quoteLine, null)
    const quoted = await renderBonus(i18nZh, newcomer, {
      rate: 0.035,
      amount: 21,
      credit: 621,
    })
    assert.equal(quoted.quoteLine, '赠送 21（3.5%），合计到账 621')
  })

  test('lists the brackets so any amount inside one is clearly covered', async () => {
    const { ranges } = await renderBonus(i18nZh, newcomer)
    assert.equal(
      ranges,
      '单笔赠送：200–499 送 2.5% · 500–999 送 3.5% · 1,000–1,999 送 5.5% · 2,000 及以上送 9%'
    )
  })

  test('English wording', async () => {
    const { cards, banner } = await renderBonus(i18n, vip2)
    assert.equal(cards[1], '200+3.5% creditPay 200 • Get 207')
    assert.match(banner, /Every top-up gets 3\.5% extra credit/)
  })
})
