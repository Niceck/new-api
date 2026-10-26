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
// 模型详情「API」页的代码示例：地址取对客的 api_base_url（不是带老端口的 server_address），
// 模型与限定入口的分组共用时下方提示那个分组的入口；模型只在限定入口的分组里时示例直接用那个入口。
// 代码块是 CodeMirror（effect 里挂载），所以用 happy-dom + createRoot 真渲染，不用 renderToStaticMarkup。
import assert from 'node:assert/strict'
import { after, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { act } from 'react'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import type { PricingModel } from '../../types'

const dom = new Window({ url: 'https://cc.yzzt.top:2053/pricing/x' })
const globals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Text',
  'Range',
  'MutationObserver',
  'ResizeObserver',
  'IntersectionObserver',
  'getComputedStyle',
  'getSelection',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'matchMedia',
  'customElements',
  'Document',
  'DocumentFragment',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'KeyboardEvent',
  'MouseEvent',
  'localStorage',
] as const
const originalDescriptors = new Map(
  globals.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
)
for (const key of globals) {
  const value = (dom as unknown as Record<string, unknown>)[key]
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value:
      typeof value === 'function' && /^[a-z]/.test(key)
        ? (value as (...args: unknown[]) => unknown).bind(dom)
        : value,
  })
}
const reactActFlag = 'IS_REACT_ACT_ENVIRONMENT'
const originalActFlag = Reflect.get(globalThis, reactActFlag)
Reflect.set(globalThis, reactActFlag, true)

const { createRoot } = await import('react-dom/client')
const { ModelDetailsApi } = await import('../model-details-api')

const i18n = createInstance()
await i18n
  .use(initReactI18next)
  // 与 src/i18n/config.ts 一致：React 自己转义，i18next 不再转义（否则 / 变成 &#x2F;）
  .init({
    lng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })

const client = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
client.setQueryData(['status'], {
  api_base_url: 'https://cc.yzzt.top',
  server_address: 'https://cc.yzzt.top:2087',
  group_entry_host: { 'claude kiro': 'kiro.yzzt.top' },
})

const container = dom.document.createElement('div')
dom.document.body.appendChild(container)
const root = createRoot(container as unknown as HTMLElement)

after(async () => {
  await act(async () => root.unmount())
  client.clear()
  await dom.happyDOM.abort()
  dom.close()
  Reflect.set(globalThis, reactActFlag, originalActFlag)
  for (const key of globals) {
    const descriptor = originalDescriptors.get(key)
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})

const haiku: PricingModel = {
  id: 1,
  model_name: 'claude-haiku-4-5-20251001',
  quota_type: 0,
  model_ratio: 0.5,
  completion_ratio: 5,
  enable_groups: ['claude kiro', '满血稳定claude'],
  supported_endpoint_types: ['anthropic'],
}

async function renderApi(model: PricingModel) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <ModelDetailsApi
            model={model}
            endpointMap={{
              anthropic: { path: '/v1/messages', method: 'POST' },
            }}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
  const code = container.querySelector('.cm-content')?.textContent ?? ''
  return { code, text: container.textContent ?? '' }
}

const KIRO_HINT =
  'Tokens in the claude kiro group must use https://kiro.yzzt.top as the base URL'

test('code samples use api_base_url, never the server_address port', async () => {
  const { code, text } = await renderApi(haiku)
  assert.ok(code.includes('https://cc.yzzt.top/v1/messages'), code)
  assert.ok(!code.includes(':2087'), code)
  assert.ok(text.includes(KIRO_HINT), text)
})

test('a model outside restricted groups shows no entry hint', async () => {
  const { code, text } = await renderApi({
    ...haiku,
    model_name: 'claude-haiku-5-5',
    enable_groups: ['满血稳定claude'],
  })
  assert.ok(code.includes('https://cc.yzzt.top/v1/messages'), code)
  assert.ok(!text.includes('Tokens in the'), text)
})

test('a model only in a restricted group samples that entry', async () => {
  const { code, text } = await renderApi({
    ...haiku,
    model_name: 'kiro-only',
    enable_groups: ['claude kiro'],
  })
  assert.ok(code.includes('https://kiro.yzzt.top/v1/messages'), code)
  assert.ok(!code.includes('cc.yzzt.top'), code)
  assert.ok(!text.includes('Tokens in the'), text)
})
