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

import { resolveDocumentTitle } from './document-title'

describe('document title once the site name is known', () => {
  test('keeps a static title that leads with the site name', () => {
    const full = 'CC API - Claude Code / Codex API 中转站｜Claude 官转约 2.5 折'
    assert.equal(resolveDocumentTitle(full, 'CC API'), full)
    assert.equal(
      resolveDocumentTitle('CC API｜中转站', 'CC API'),
      'CC API｜中转站'
    )
    assert.equal(
      resolveDocumentTitle('  CC API · 中转站  ', ' CC API '),
      'CC API · 中转站'
    )
  })

  test('replaces the stock title with the site name', () => {
    assert.equal(resolveDocumentTitle('New API', 'CC API'), 'CC API')
    assert.equal(resolveDocumentTitle('', 'CC API'), 'CC API')
  })

  test('replaces a static title written for a different site name', () => {
    // 部署方后来在后台改了站点名：不能再挂着镜像里写死的旧标题
    assert.equal(
      resolveDocumentTitle('CC API - Claude Code 中转站', 'Other Gateway'),
      'Other Gateway'
    )
  })

  test('a shortened site name does not keep the old title', () => {
    assert.equal(
      resolveDocumentTitle('CC API - Claude Code 中转站', 'CC'),
      'CC'
    )
  })

  test('a longer name sharing the prefix is not the same site', () => {
    assert.equal(resolveDocumentTitle('CC APIX - 中转站', 'CC API'), 'CC API')
  })

  test('static title equal to the name stays as is', () => {
    assert.equal(resolveDocumentTitle('CC API', 'CC API'), 'CC API')
  })

  test('an empty site name never blanks the title', () => {
    assert.equal(
      resolveDocumentTitle('CC API - 中转站', '   '),
      'CC API - 中转站'
    )
  })
})
