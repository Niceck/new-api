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
import { after, before, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import { CustomFooter, ProjectAttribution } from '../footer'

// 不用顶层 await：这个文件的依赖图很大，顶层 await 挂起期间 Bun 会先去跑后面的测试文件，
// 等回来再登记用例时就被 node:test 兼容层判成「测试里嵌套测试」，整份文件报错（只在全量运行时出现）。
const dom = new Window()
const i18n = createInstance()
const client = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})

before(async () => {
  await i18n
    .use(initReactI18next)
    .init({ lng: 'en', resources: { en: { translation: {} } } })
})

after(() => {
  client.clear()
  dom.close()
})

// NOTICE（AGPLv3 第 7(b) 条）要求原样保留的署名，以及必须可见的项目链接。
const REQUIRED_NOTICE =
  'Frontend design and development by New API contributors.'
const PROJECT_URL = 'https://github.com/QuantumNous/new-api'
const YEAR = 2026

function render(node: React.ReactNode) {
  const element = dom.document.createElement('div')
  element.innerHTML = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>{node}</I18nextProvider>
    </QueryClientProvider>
  )
  const text = (element.textContent ?? '').replaceAll(/\s+/g, ' ').trim()
  return { element, text }
}

function renderSiteFooter() {
  return render(
    <CustomFooter
      footerHtml='CC API - Claude Code 企业级中转'
      displayName='CC API'
      currentYear={YEAR}
    />
  )
}

test('署名：原样显示许可证要求的句子，不翻译、不改写', () => {
  const { text } = render(<ProjectAttribution />)
  assert.equal(text, REQUIRED_NOTICE)
})

test('署名：项目名是指向原项目的可见链接', () => {
  const { element } = render(<ProjectAttribution />)
  const link = element.querySelector('a')
  assert.ok(link, '缺少项目链接')
  assert.equal(link.getAttribute('href'), PROJECT_URL)
  assert.equal(link.textContent, 'New API')
  assert.equal(link.getAttribute('rel'), 'noopener noreferrer')
})

test('署名：不再把原项目写成本站的版权方', () => {
  const { text } = render(<ProjectAttribution />)
  assert.ok(!text.includes('©'), text)
  assert.ok(!/All rights reserved|版权所有/.test(text), text)
})

test('站点自定义页脚：版权行写的是本站名称', () => {
  const { text } = renderSiteFooter()
  assert.ok(text.includes(`© ${YEAR} CC API`), text)
  assert.ok(!text.includes(`© ${YEAR} New API`), text)
})

test('站点自定义页脚：站点文案照常显示', () => {
  const { text } = renderSiteFooter()
  assert.ok(text.includes('CC API - Claude Code 企业级中转'), text)
})

test('站点自定义页脚：许可证要求的署名与项目链接仍在页脚里', () => {
  const { element, text } = renderSiteFooter()
  assert.ok(text.includes(REQUIRED_NOTICE), text)
  const links = [...element.querySelectorAll('footer a')].map((a) =>
    a.getAttribute('href')
  )
  assert.ok(links.includes(PROJECT_URL), JSON.stringify(links))
})
