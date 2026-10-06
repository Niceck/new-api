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
import { api, type ApiRequestConfig } from '@/lib/api'

import type { HomePageContentResponse } from './types'

// ============================================================================
// Home Page APIs
// ============================================================================

/**
 * Get custom home page content
 * Returns Markdown/HTML content or iframe URL
 */
export async function getHomePageContent(): Promise<HomePageContentResponse> {
  const res = await api.get('/api/home_page_content')
  return res.data
}

const HOME_PRICING_TIMEOUT_MS = 8000

type PricingGet = (
  url: string,
  config: ApiRequestConfig
) => Promise<{ data: unknown }>

/**
 * Fetch the public pricing payload for the home page price table.
 *
 * Returned as unknown on purpose: the home page validates it field by field
 * before showing any price.
 *
 * A failure here must stay inside the price section, which then points to
 * the full price table. So the request opts out of the global toasts and of
 * the session refresh, never joins an in-flight /api/pricing request made
 * elsewhere without those options, and the error is rethrown as a plain
 * Error: an AxiosError with status 500 would make the query cache send the
 * whole page to /500.
 */
export async function fetchHomePricing(get: PricingGet): Promise<unknown> {
  try {
    const res = await get('/api/pricing', {
      skipErrorHandler: true,
      skipBusinessError: true,
      skipAuthRefresh: true,
      disableDuplicate: true,
      timeout: HOME_PRICING_TIMEOUT_MS,
    })
    return res.data
  } catch (error) {
    throw new Error('Home page pricing is unavailable', { cause: error })
  }
}

export function getHomePricing(): Promise<unknown> {
  return fetchHomePricing((url, config) => api.get(url, config))
}
