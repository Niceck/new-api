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
import { useMemo } from 'react'

import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'

import { resolveSiteBase, resolveTokenApiBase } from '../lib/token-api-base'

/**
 * 某个令牌的接入地址（apiBase）与站点地址（siteBase），规则见 lib/token-api-base。
 */
export function useTokenApiBase(tokenGroup?: string | null): {
  apiBase: string
  siteBase: string
} {
  const { status } = useStatus()
  const userGroup = useAuthStore((state) => state.auth.user?.group)
  const origin = typeof window === 'undefined' ? '' : window.location.origin

  return useMemo(
    () => ({
      apiBase: resolveTokenApiBase({ status, tokenGroup, userGroup, origin }),
      siteBase: resolveSiteBase(status, origin),
    }),
    [status, tokenGroup, userGroup, origin]
  )
}
