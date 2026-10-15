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
// 后台「聊天」设置里可以写一条 {"CC Switch": "ccswitch"}：它不是链接模板，
// 表示「打开 CC Switch 导入对话框」（旧前端的约定）。
export function isCCSwitchPreset(url: string): boolean {
  return url.trim().toLowerCase() === 'ccswitch'
}

// CC Switch 的一键导入链接。endpoint 用令牌的接入地址（Codex 走 OpenAI 协议，要带 /v1），
// homepage 用站点地址：分组专用入口只转发模型接口，打开是一页 404。
export function buildCCSwitchURL(params: {
  app: string
  name: string
  models: Record<string, string>
  apiKey: string
  apiBase: string
  homepage: string
}): string {
  const endpoint =
    params.app === 'codex' ? `${params.apiBase}/v1` : params.apiBase
  const query = new URLSearchParams()
  query.set('resource', 'provider')
  query.set('app', params.app)
  query.set('name', params.name)
  query.set('endpoint', endpoint)
  query.set('apiKey', params.apiKey)
  for (const [k, v] of Object.entries(params.models)) {
    if (v) query.set(k, v)
  }
  query.set('homepage', params.homepage)
  query.set('enabled', 'true')
  return `ccswitch://v1/import?${query.toString()}`
}
