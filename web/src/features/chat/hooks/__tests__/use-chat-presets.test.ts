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

import { extractServerAddress } from '../use-chat-presets'

describe('extractServerAddress', () => {
  test('prefers api_base_url over server_address', () => {
    assert.equal(
      extractServerAddress({
        api_base_url: 'https://cc.yzzt.top',
        server_address: 'https://cc.yzzt.top:2087',
      }),
      'https://cc.yzzt.top'
    )
  })

  test('skips an empty api_base_url instead of stopping on it', () => {
    assert.equal(
      extractServerAddress({
        api_base_url: '',
        server_address: 'https://cc.yzzt.top:2087',
      }),
      'https://cc.yzzt.top:2087'
    )
    assert.equal(
      extractServerAddress({
        data: { api_base_url: '  ', server_address: 'https://a.example.com' },
      }),
      'https://a.example.com'
    )
  })
})
