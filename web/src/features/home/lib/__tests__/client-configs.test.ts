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

import {
  API_BASE_URL,
  API_KEY_PLACEHOLDER,
  buildClientConfigs,
  type ClientConfig,
} from '../client-configs.ts'

const configs = buildClientConfigs(API_BASE_URL)
const byId = new Map(configs.map((config) => [config.id, config]))
const allCode = configs
  .flatMap((config) => config.blocks.map((block) => block.code))
  .join('\n')

function codeOf(id: ClientConfig['id'], index = 0): string {
  const config = byId.get(id)
  assert.ok(config, `缺少客户端配置：${id}`)
  return config.blocks[index].code
}

describe('buildClientConfigs', () => {
  test('只给站点真实支持的三种接入方式，顺序固定', () => {
    assert.deepEqual(
      configs.map((config) => config.id),
      ['claude-code', 'codex', 'openai-chat']
    )
  })

  test('对客地址不带端口，也不出现站内没有的 Gemini', () => {
    assert.equal(API_BASE_URL, 'https://cc.yzzt.top')
    assert.ok(!allCode.includes(':2087'))
    assert.ok(!/gemini|generateContent/i.test(allCode))
  })

  test('Claude Code：settings.json 可解析，地址不带 /v1，只设 AUTH_TOKEN', () => {
    assert.equal(
      codeOf('claude-code', 0),
      'npm install -g @anthropic-ai/claude-code'
    )
    const parsed = JSON.parse(codeOf('claude-code', 1)) as {
      env: Record<string, string>
    }
    assert.equal(parsed.env.ANTHROPIC_BASE_URL, 'https://cc.yzzt.top')
    assert.equal(parsed.env.ANTHROPIC_AUTH_TOKEN, API_KEY_PLACEHOLDER)
    assert.ok(!('ANTHROPIC_API_KEY' in parsed.env))
  })

  test('Codex：base_url 带 /v1，协议为 responses，密钥走环境变量', () => {
    assert.equal(codeOf('codex', 0), 'npm install -g @openai/codex')
    const toml = codeOf('codex', 1)
    assert.ok(toml.includes('base_url = "https://cc.yzzt.top/v1"'))
    assert.ok(toml.includes('wire_api = "responses"'))
    assert.ok(toml.includes('env_key = "HHAPI_KEY"'))
    assert.ok(!toml.includes(API_KEY_PLACEHOLDER))
    assert.equal(
      codeOf('codex', 2),
      `export HHAPI_KEY="${API_KEY_PLACEHOLDER}"`
    )
  })

  test('OpenAI Chat：给完整接口地址、Base URL 与 GPT-6 模型名', () => {
    assert.equal(
      codeOf('openai-chat', 0),
      'https://cc.yzzt.top/v1/chat/completions'
    )
    assert.equal(codeOf('openai-chat', 1), 'https://cc.yzzt.top/v1')
    assert.equal(codeOf('openai-chat', 2), 'gpt-6-sol')
  })

  test('没有一段示例是打 Claude 接口的裸请求', () => {
    assert.ok(!allCode.includes('/v1/messages'))
    assert.ok(!/\bcurl\b/.test(allCode))
  })

  test('每种接入方式都带站内教程链接，且不占用后端接口前缀', () => {
    for (const config of configs) {
      assert.match(config.guideHref, /^\/[a-z]/)
      assert.ok(!/^\/(api|v1|assets)/.test(config.guideHref))
      assert.ok(config.blocks.length > 0)
    }
  })

  test('地址末尾的斜杠不会拼出双斜杠', () => {
    const trailing = buildClientConfigs('https://example.test/')
    const code = trailing
      .flatMap((config) => config.blocks.map((block) => block.code))
      .join('\n')
    assert.ok(!code.includes('test//'))
    assert.ok(code.includes('https://example.test/v1'))
  })
})
