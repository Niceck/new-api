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

import { resolveSiteBase, resolveTokenApiBase } from '../token-api-base'

const ORIGIN = 'https://cc.yzzt.top:2053'

const STATUS = {
  api_base_url: 'https://cc.yzzt.top',
  server_address: 'https://cc.yzzt.top:2087',
  group_entry_host: { 'claude kiro': 'kiro.yzzt.top' },
}

describe('resolveSiteBase', () => {
  test('prefers api_base_url over server_address and the page origin', () => {
    assert.equal(resolveSiteBase(STATUS, ORIGIN), 'https://cc.yzzt.top')
  })

  test('falls back to server_address, then to the page origin', () => {
    assert.equal(
      resolveSiteBase({ server_address: 'https://cc.yzzt.top:2087' }, ORIGIN),
      'https://cc.yzzt.top:2087'
    )
    assert.equal(resolveSiteBase({}, ORIGIN), ORIGIN)
    assert.equal(resolveSiteBase(null, ORIGIN), ORIGIN)
  })

  test('reads fields nested under data', () => {
    assert.equal(
      resolveSiteBase(
        { data: { api_base_url: 'https://cc.yzzt.top' } },
        ORIGIN
      ),
      'https://cc.yzzt.top'
    )
  })

  test('ignores blank or non-string values and trims trailing slashes', () => {
    assert.equal(
      resolveSiteBase(
        { api_base_url: '  ', server_address: 'https://a.example.com//' },
        ORIGIN
      ),
      'https://a.example.com'
    )
    assert.equal(resolveSiteBase({ api_base_url: 42 }, ORIGIN), ORIGIN)
  })
})

describe('resolveTokenApiBase', () => {
  const base = (tokenGroup?: string | null, userGroup?: string | null) =>
    resolveTokenApiBase({
      status: STATUS,
      tokenGroup,
      userGroup,
      origin: ORIGIN,
    })

  test('a group with a dedicated entry uses that entry', () => {
    assert.equal(base('claude kiro'), 'https://kiro.yzzt.top')
  })

  test('other groups use the site base', () => {
    assert.equal(base('满血稳定claude'), 'https://cc.yzzt.top')
    assert.equal(base('满血稳定codex', 'claude kiro'), 'https://cc.yzzt.top')
  })

  test('a token without a group follows the account group', () => {
    assert.equal(base('', 'claude kiro'), 'https://kiro.yzzt.top')
    assert.equal(base(null, 'claude kiro'), 'https://kiro.yzzt.top')
    assert.equal(base('', '满血稳定claude'), 'https://cc.yzzt.top')
    assert.equal(base('', null), 'https://cc.yzzt.top')
  })

  test('auto tokens and missing account groups use the site base', () => {
    assert.equal(base('auto', 'claude kiro'), 'https://cc.yzzt.top')
    assert.equal(base(undefined, undefined), 'https://cc.yzzt.top')
  })

  test('the dedicated entry drops the site port', () => {
    const status = { group_entry_host: STATUS.group_entry_host }
    const withPort = { ...status, server_address: 'https://cc.yzzt.top:2087' }
    assert.equal(
      resolveTokenApiBase({
        status: withPort,
        tokenGroup: 'claude kiro',
        origin: ORIGIN,
      }),
      'https://kiro.yzzt.top'
    )
  })

  test('the dedicated entry keeps the site protocol', () => {
    const status = { ...STATUS, api_base_url: 'http://localhost:3000' }
    assert.equal(
      resolveTokenApiBase({
        status,
        tokenGroup: 'claude kiro',
        origin: ORIGIN,
      }),
      'http://kiro.yzzt.top'
    )
  })

  test('malformed group_entry_host values are ignored', () => {
    for (const entries of [
      ['kiro.yzzt.top'],
      'kiro.yzzt.top',
      { 'claude kiro': 123 },
      { 'claude kiro': '' },
      { 'claude kiro': 'evil.example.com/path' },
      { 'claude kiro': 'kiro.yzzt.top:2087' },
      { 'claude kiro': 'a b' },
      { 'claude kiro': `${'a'.repeat(250)}.com` },
    ]) {
      const status = { ...STATUS, group_entry_host: entries }
      assert.equal(
        resolveTokenApiBase({
          status,
          tokenGroup: 'claude kiro',
          origin: ORIGIN,
        }),
        'https://cc.yzzt.top',
        JSON.stringify(entries)
      )
    }
  })

  test('an older backend without the new fields keeps the upstream behaviour', () => {
    const status = { server_address: 'https://cc.yzzt.top:2087' }
    assert.equal(
      resolveTokenApiBase({
        status,
        tokenGroup: 'claude kiro',
        origin: ORIGIN,
      }),
      'https://cc.yzzt.top:2087'
    )
  })
})
