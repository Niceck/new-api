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

import { buildCCSwitchURL, isCCSwitchPreset } from '../cc-switch-url'

function params(url: string): URLSearchParams {
  const prefix = 'ccswitch://v1/import?'
  assert.ok(url.startsWith(prefix), url)
  return new URLSearchParams(url.slice(prefix.length))
}

const COMMON = {
  name: 'My Claude',
  apiKey: 'sk-test',
  homepage: 'https://cc.yzzt.top',
}

describe('buildCCSwitchURL', () => {
  test('claude endpoint is the token api base without /v1', () => {
    const p = params(
      buildCCSwitchURL({
        ...COMMON,
        app: 'claude',
        apiBase: 'https://kiro.yzzt.top',
        models: { model: 'claude-opus-5-5', haikuModel: '' },
      })
    )
    assert.equal(p.get('endpoint'), 'https://kiro.yzzt.top')
    assert.equal(p.get('app'), 'claude')
    assert.equal(p.get('apiKey'), 'sk-test')
    assert.equal(p.get('model'), 'claude-opus-5-5')
    assert.equal(p.has('haikuModel'), false)
    assert.equal(p.get('enabled'), 'true')
    assert.equal(p.get('resource'), 'provider')
  })

  test('codex endpoint appends /v1', () => {
    const p = params(
      buildCCSwitchURL({
        ...COMMON,
        app: 'codex',
        apiBase: 'https://cc.yzzt.top',
        models: { model: 'gpt-6-sol' },
      })
    )
    assert.equal(p.get('endpoint'), 'https://cc.yzzt.top/v1')
  })

  test('homepage is the site, not the dedicated entry', () => {
    const p = params(
      buildCCSwitchURL({
        ...COMMON,
        app: 'claude',
        apiBase: 'https://kiro.yzzt.top',
        models: { model: 'claude-opus-5-5' },
      })
    )
    assert.equal(p.get('homepage'), 'https://cc.yzzt.top')
  })
})

describe('isCCSwitchPreset', () => {
  test('matches the bare ccswitch chat preset in any case', () => {
    assert.equal(isCCSwitchPreset('ccswitch'), true)
    assert.equal(isCCSwitchPreset(' CCSwitch '), true)
  })

  test('does not match real link templates', () => {
    assert.equal(isCCSwitchPreset('ccswitch://v1/import?app=claude'), false)
    assert.equal(isCCSwitchPreset('cherrystudio://providers/api-keys'), false)
    assert.equal(isCCSwitchPreset('fluentread'), false)
    assert.equal(isCCSwitchPreset(''), false)
  })
})
