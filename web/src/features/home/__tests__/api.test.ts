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
import { describe, test } from 'node:test'

import { AxiosError } from 'axios'

import type { ApiRequestConfig } from '@/lib/api'

import { fetchHomePricing } from '../api'

type Call = { url: string; config: ApiRequestConfig }

function recordingGet(result: () => Promise<{ data: unknown }>) {
  const calls: Call[] = []
  const get = (url: string, config: ApiRequestConfig) => {
    calls.push({ url, config })
    return result()
  }
  return { calls, get }
}

describe('fetchHomePricing', () => {
  test('成功时原样返回响应体', async () => {
    const payload = { success: true, data: [] }
    const { get } = recordingGet(async () => ({ data: payload }))
    assert.equal(await fetchHomePricing(get), payload)
  })

  test('请求不走全局提示、不触发会话刷新、不复用别处的在途请求，并且有超时', async () => {
    const { calls, get } = recordingGet(async () => ({ data: {} }))
    await fetchHomePricing(get)
    assert.equal(calls.length, 1)
    assert.equal(calls[0].url, '/api/pricing')
    assert.equal(calls[0].config.skipErrorHandler, true)
    assert.equal(calls[0].config.skipBusinessError, true)
    assert.equal(calls[0].config.skipAuthRefresh, true)
    assert.equal(calls[0].config.disableDuplicate, true)
    assert.equal(calls[0].config.timeout, 8000)
  })

  test('500 不会以 AxiosError 的形态抛出（否则全局查询缓存会整页跳 /500）', async () => {
    const failure = new AxiosError(
      'boom',
      'ERR_BAD_RESPONSE',
      undefined,
      null,
      {
        status: 500,
        statusText: 'Internal Server Error',
        data: {},
        headers: {},
        config: {} as never,
      }
    )
    const { get } = recordingGet(async () => {
      throw failure
    })
    await assert.rejects(fetchHomePricing(get), (error: unknown) => {
      assert.ok(error instanceof Error)
      assert.ok(!(error instanceof AxiosError))
      assert.equal((error as Error).cause, failure)
      return true
    })
  })
})
