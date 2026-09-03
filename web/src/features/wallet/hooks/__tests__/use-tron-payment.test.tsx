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

import type { TronTopupOrder } from '../../types'

const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Node',
  'Element',
  'Event',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { useTronPayment, isValidTronTxID } = await import('../use-tron-payment')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const pendingOrder: TronTopupOrder = {
  trade_no: 'TRON-order-1',
  network: 'mainnet',
  receive_address: 'TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f',
  token_contract: 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t',
  expected_usdt_micros: 13_791_234,
  rate_cny_micros: 7_250_000,
  quote_updated_at_ms: 1_800_000_000_000,
  expires_at_ms: 1_800_001_200_000,
  credit_quota: 50_000_000,
  status: 'pending',
}

describe('useTronPayment', () => {
  after(() => domWindow.close())

  test('creates an order and polls only while the dialog is pending', async () => {
    let timerCallback: (() => void | Promise<void>) | undefined
    let cleared = 0
    let successCalls = 0
    let latest: ReturnType<typeof useTronPayment> | undefined
    const dependencies = {
      createOrder: async (amount: number) => {
        assert.equal(amount, 100)
        return { success: true, data: pendingOrder }
      },
      getOrder: async (tradeNo: string) => {
        assert.equal(tradeNo, pendingOrder.trade_no)
        return {
          success: true,
          data: { ...pendingOrder, status: 'success' as const },
        }
      },
      submitClaim: async () => ({ success: true, data: { id: 1 } }),
      schedule: (callback: () => void | Promise<void>) => {
        timerCallback = callback
        return 7
      },
      cancelSchedule: (id: number) => {
        assert.equal(id, 7)
        cleared++
      },
      pollDelayMS: 5_000,
    }

    function Harness() {
      latest = useTronPayment({
        dependencies,
        onSuccess: () => {
          successCalls++
        },
      })
      return <div data-status={latest.order?.status ?? 'none'} />
    }

    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => root.render(<Harness />))

    await act(async () => {
      await latest?.startPayment(100)
    })
    assert.equal(latest?.open, true)
    assert.equal(latest?.order?.trade_no, pendingOrder.trade_no)
    assert.ok(timerCallback)

    await act(async () => {
      await timerCallback?.()
    })
    assert.equal(latest?.order?.status, 'success')
    assert.equal(successCalls, 1)
    assert.equal(cleared > 0, true)

    await act(async () => root.unmount())
    container.remove()
  })

  test('validates canonical transaction ids before claim submission', () => {
    assert.equal(isValidTronTxID('a'.repeat(64)), true)
    assert.equal(isValidTronTxID('A'.repeat(64)), true)
    assert.equal(isValidTronTxID('g'.repeat(64)), false)
    assert.equal(isValidTronTxID('a'.repeat(63)), false)
  })

  test('retries business and transport polling failures with bounded scheduling', async () => {
    const callbacks: Array<() => void | Promise<void>> = []
    const delays: number[] = []
    let pollCalls = 0
    let latest: ReturnType<typeof useTronPayment> | undefined
    const dependencies = {
      createOrder: async () => ({ success: true, data: pendingOrder }),
      getOrder: async () => {
        pollCalls++
        if (pollCalls === 1) return { success: false, message: 'temporary' }
        if (pollCalls === 2) throw new Error('transport')
        return { success: true, data: pendingOrder }
      },
      submitClaim: async () => ({ success: true, data: { id: 1 } }),
      schedule: (callback: () => void | Promise<void>, delayMS: number) => {
        callbacks.push(callback)
        delays.push(delayMS)
        return callbacks.length
      },
      cancelSchedule: () => undefined,
      pollDelayMS: 5_000,
    }

    function Harness() {
      latest = useTronPayment({ dependencies })
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () => root.render(<Harness />))
    await act(async () => {
      await latest?.startPayment(100)
    })
    assert.equal(callbacks.length, 1)
    await act(async () => callbacks[0]?.())
    assert.equal(callbacks.length, 2)
    await act(async () => callbacks[1]?.())
    assert.equal(callbacks.length, 3)
    assert.deepEqual(delays, [5_000, 10_000, 20_000])

    await act(async () => root.unmount())
  })

  test('ignores a successful stale response after the dialog closes', async () => {
    let resolvePoll:
      | ((response: { success: true; data: TronTopupOrder }) => void)
      | undefined
    let timerCallback: (() => void | Promise<void>) | undefined
    let successCalls = 0
    let latest: ReturnType<typeof useTronPayment> | undefined
    const dependencies = {
      createOrder: async () => ({ success: true, data: pendingOrder }),
      getOrder: () =>
        new Promise<{ success: true; data: TronTopupOrder }>((resolve) => {
          resolvePoll = resolve
        }),
      submitClaim: async () => ({ success: true, data: { id: 1 } }),
      schedule: (callback: () => void | Promise<void>) => {
        timerCallback = callback
        return 1
      },
      cancelSchedule: () => undefined,
      pollDelayMS: 5_000,
    }

    function Harness() {
      latest = useTronPayment({
        dependencies,
        onSuccess: () => {
          successCalls++
        },
      })
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () => root.render(<Harness />))
    await act(async () => {
      await latest?.startPayment(100)
    })
    let pendingPoll: void | Promise<void> | undefined
    await act(async () => {
      pendingPoll = timerCallback?.()
    })
    await act(async () => latest?.onOpenChange(false))
    resolvePoll?.({
      success: true,
      data: { ...pendingOrder, status: 'success' },
    })
    await act(async () => pendingPoll)

    assert.equal(latest?.order?.status, 'pending')
    assert.equal(successCalls, 0)
    await act(async () => root.unmount())
  })
})
