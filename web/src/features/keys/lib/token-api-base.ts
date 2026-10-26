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
// 令牌的接入地址：令牌页「复制服务器地址」、CC Switch 一键导入、聊天预设的 {address} 都用它。
//
// - 站点地址：后台的对客接口地址 api_base_url，没设时退回 server_address，再退回当前页面地址。
//   server_address 同时是支付回调地址，站点可能有意让它和对客地址不同（例如带端口）。
// - 分组限定了入口（/api/status 的 group_entry_host）时，这个分组的令牌只能走那个入口，
//   在别的地址会被拒绝，所以用那个入口的主机名，协议沿用站点地址的。
// - 令牌没选分组时按账号分组计费和限入口（与后端一致），这里也按账号分组算。
//
// 模型广场的代码示例不针对某个令牌，按模型所在的分组给地址，见 resolveModelApiBase。

type StatusLike = Record<string, unknown> | null | undefined

// 与后端 group_entry_hosts 的规范写法一致：小写、按点分段、不带端口与路径。
const ENTRY_HOST_RE =
  /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/

function readField(status: StatusLike, key: string): unknown {
  if (!status) return undefined
  if (status[key] !== undefined) return status[key]
  const data = status.data
  if (data && typeof data === 'object') {
    return (data as Record<string, unknown>)[key]
  }
  return undefined
}

function readAddress(status: StatusLike, key: string): string {
  const value = readField(status, key)
  if (typeof value !== 'string') return ''
  return value.trim().replace(/\/+$/, '')
}

export function resolveSiteBase(status: StatusLike, origin: string): string {
  return (
    readAddress(status, 'api_base_url') ||
    readAddress(status, 'server_address') ||
    origin
  )
}

function resolveEntryHost(status: StatusLike, group: string): string {
  if (!group) return ''
  const entries = readField(status, 'group_entry_host')
  if (!entries || typeof entries !== 'object' || Array.isArray(entries)) {
    return ''
  }
  const host = (entries as Record<string, unknown>)[group]
  if (
    typeof host !== 'string' ||
    host.length > 253 ||
    !ENTRY_HOST_RE.test(host)
  ) {
    return ''
  }
  return host
}

function protocolOf(address: string): string {
  try {
    const { protocol } = new URL(address)
    return protocol === 'http:' ? 'http:' : 'https:'
  } catch {
    return 'https:'
  }
}

export function resolveTokenApiBase(params: {
  status: StatusLike
  tokenGroup?: string | null
  userGroup?: string | null
  origin: string
}): string {
  const siteBase = resolveSiteBase(params.status, params.origin)
  const group = params.tokenGroup || params.userGroup || ''
  const host = resolveEntryHost(params.status, group)
  if (!host) return siteBase
  return `${protocolOf(siteBase)}//${host}`
}

export type GroupEntryHint = { group: string; apiBase: string }

/**
 * 模型广场代码示例的接入地址（不针对某个令牌，按模型所在的分组）：
 * - 模型所在的分组全都限定了同一个入口：模型只能在那里调用，用那个入口；
 * - 否则用站点地址，并把限定了入口的分组列出来（这些分组的令牌要改用各自的入口）。
 * groups 来自接口数据，逐项校验，非字符串与空串忽略、重复的只算一次。
 */
export function resolveModelApiBase(params: {
  status: StatusLike
  groups: unknown
  origin: string
}): { apiBase: string; entryHints: GroupEntryHint[] } {
  const siteBase = resolveSiteBase(params.status, params.origin)
  const raw = Array.isArray(params.groups) ? params.groups : []
  const groups = [
    ...new Set(
      raw.filter((g): g is string => typeof g === 'string' && g !== '')
    ),
  ]
  const protocol = protocolOf(siteBase)
  const restricted = groups
    .map((group) => ({ group, host: resolveEntryHost(params.status, group) }))
    .filter((entry) => entry.host !== '')
  const hosts = new Set(restricted.map((entry) => entry.host))
  if (
    groups.length > 0 &&
    restricted.length === groups.length &&
    hosts.size === 1
  ) {
    return { apiBase: `${protocol}//${restricted[0].host}`, entryHints: [] }
  }
  return {
    apiBase: siteBase,
    entryHints: restricted.map((entry) => ({
      group: entry.group,
      apiBase: `${protocol}//${entry.host}`,
    })),
  }
}
