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
/**
 * Copy-ready client setup shown in the home page hero.
 *
 * Only the three ways of connecting that the site actually supports are
 * listed, and none of them is a raw request against the Claude endpoint:
 * the Claude lines are only guaranteed for the official Claude Code client.
 */

/** Customer-facing API address (no port). */
export const API_BASE_URL = 'https://cc.yzzt.top'

export const API_KEY_PLACEHOLDER = 'YOUR_API_KEY'

export type ClientConfigBlock = {
  /** File path shown as-is, or an i18n key when `translateTitle` is set */
  title: string
  translateTitle: boolean
  code: string
}

export type ClientConfig = {
  id: 'claude-code' | 'codex' | 'openai-chat'
  /** Product name, shown untranslated */
  label: string
  blocks: ClientConfigBlock[]
  /** i18n key */
  note: string
  guideHref: string
}

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, '')
}

function claudeCodeConfig(base: string): ClientConfig {
  const settings = {
    env: {
      ANTHROPIC_BASE_URL: base,
      ANTHROPIC_AUTH_TOKEN: API_KEY_PLACEHOLDER,
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
    },
  }
  return {
    id: 'claude-code',
    label: 'Claude Code',
    blocks: [
      {
        title: 'Install the official client',
        translateTitle: true,
        code: 'npm install -g @anthropic-ai/claude-code',
      },
      {
        title: '~/.claude/settings.json',
        translateTitle: false,
        code: JSON.stringify(settings, null, 2),
      },
    ],
    note: 'Merge env into your existing settings; do not replace the file. Use a token from a Claude group. The address has no /v1.',
    guideHref: '/claude-code',
  }
}

function codexConfig(base: string): ClientConfig {
  const toml = [
    'model_provider = "hhapi"',
    'model = "gpt-6-sol"',
    'model_reasoning_effort = "xhigh"',
    '',
    '[model_providers.hhapi]',
    'name = "CC API"',
    `base_url = "${base}/v1"`,
    'wire_api = "responses"',
    'env_key = "HHAPI_KEY"',
  ].join('\n')
  return {
    id: 'codex',
    label: 'Codex CLI',
    blocks: [
      {
        title: 'Install the official client',
        translateTitle: true,
        code: 'npm install -g @openai/codex',
      },
      { title: '~/.codex/config.toml', translateTitle: false, code: toml },
      {
        title: 'Environment variable (add it to your shell profile to keep it)',
        translateTitle: true,
        code: `export HHAPI_KEY="${API_KEY_PLACEHOLDER}"`,
      },
    ],
    note: 'Use a token from the GPT-6 group. The address ends with /v1.',
    guideHref: '/codex-api',
  }
}

function openAiChatConfig(base: string): ClientConfig {
  return {
    id: 'openai-chat',
    label: 'OpenAI Chat',
    blocks: [
      {
        title: 'Endpoint URL',
        translateTitle: true,
        code: `${base}/v1/chat/completions`,
      },
      {
        title: 'Base URL, for clients that only ask for one',
        translateTitle: true,
        code: `${base}/v1`,
      },
      { title: 'Model name', translateTitle: true, code: 'gpt-6-sol' },
    ],
    note: 'GPT-6 models only. Claude model names are rejected on this protocol.',
    guideHref: '/codex-api',
  }
}

export function buildClientConfigs(apiBase: string): ClientConfig[] {
  const base = trimTrailingSlash(apiBase)
  return [claudeCodeConfig(base), codexConfig(base), openAiChatConfig(base)]
}
