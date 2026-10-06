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
import { useIsFetching, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

import { useStatus } from '@/hooks/use-status'
import { mapStatusDataToConfig } from '@/hooks/use-system-config'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { getHomePricing } from '../api'
import { HEADLINE_MODELS, LINE_ORDER } from '../constants'
import {
  confirmCurrency,
  isAwaitingCurrency,
  resolveHomePricing,
  type HomePricing,
} from '../lib/home-pricing-state'
import { buildPriceLines } from '../lib/price-table'

const PRICE_LINE_OPTIONS = {
  headlineModels: HEADLINE_MODELS,
  lineOrder: LINE_ORDER,
}

// How long prices wait for the display currency before giving way to the
// link to the full price table. They come back if the currency turns up.
const CURRENCY_WAIT_MS = 8000

type StatusData = Parameters<typeof mapStatusDataToConfig>[0]

/** True once `active` has stayed true for `ms`; resets when it turns false. */
function useExpired(active: boolean, ms: number): boolean {
  const [expired, setExpired] = useState(false)

  useEffect(() => {
    if (!active) return
    const timer = setTimeout(() => setExpired(true), ms)
    return () => {
      clearTimeout(timer)
      setExpired(false)
    }
  }, [active, ms])

  return active && expired
}

/**
 * Live prices for the home page, shared by every section that needs them
 * (one request per page view). Lines come with the display currency they
 * must be formatted in, and only once that currency is confirmed.
 */
export function useHomePricing(): HomePricing {
  const query = useQuery({
    queryKey: ['home', 'pricing'],
    queryFn: getHomePricing,
    staleTime: 5 * 60 * 1000,
    retry: 1,
  })
  const { status } = useStatus()
  const statusFetching = useIsFetching({ queryKey: ['status'] }) > 0
  const storedCurrency = useSystemConfigStore((s) => s.config.currency)

  const lines = useMemo(
    () => buildPriceLines(query.data, PRICE_LINE_OPTIONS),
    [query.data]
  )
  const currency = useMemo(() => {
    // Same mapping the status hook uses to fill the store.
    const fromStatus = status
      ? (mapStatusDataToConfig(status as StatusData).currency ?? null)
      : null
    return confirmCurrency(fromStatus, storedCurrency)
  }, [status, storedCurrency])

  const wait = useMemo(
    () => ({
      lines,
      pricingLoading: query.isLoading,
      currency,
      statusFetching,
    }),
    [lines, query.isLoading, currency, statusFetching]
  )
  const currencyWaitExpired = useExpired(
    isAwaitingCurrency(wait),
    CURRENCY_WAIT_MS
  )

  return useMemo(
    () => resolveHomePricing({ ...wait, currencyWaitExpired }),
    [wait, currencyWaitExpired]
  )
}
