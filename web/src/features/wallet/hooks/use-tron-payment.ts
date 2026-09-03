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
import i18next from 'i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import {
  createTronTopupOrder,
  getTronTopupOrder,
  isApiSuccess,
  submitTronTopupClaim,
} from '../api'
import { isValidTronTxID } from '../lib/tron'
import type {
  TronTopupClaimResponse,
  TronTopupOrder,
  TronTopupOrderResponse,
} from '../types'

type ScheduledCallback = () => void | Promise<void>

export interface TronPaymentDependencies {
  createOrder: (amount: number) => Promise<TronTopupOrderResponse>
  getOrder: (tradeNo: string) => Promise<TronTopupOrderResponse>
  submitClaim: (request: {
    trade_no: string
    tx_id: string
    note: string
  }) => Promise<TronTopupClaimResponse>
  schedule: (callback: ScheduledCallback, delayMS: number) => number
  cancelSchedule: (scheduleID: number) => void
  pollDelayMS: number
}

interface UseTronPaymentOptions {
  dependencies?: TronPaymentDependencies
  onSuccess?: () => void | Promise<void>
}

const defaultTronPaymentDependencies: TronPaymentDependencies = {
  createOrder: createTronTopupOrder,
  getOrder: getTronTopupOrder,
  submitClaim: submitTronTopupClaim,
  schedule: (callback, delayMS) =>
    window.setTimeout(() => {
      void callback()
    }, delayMS),
  cancelSchedule: (scheduleID) => window.clearTimeout(scheduleID),
  pollDelayMS: 5_000,
}

export { isValidTronTxID } from '../lib/tron'

export function useTronPayment(options: UseTronPaymentOptions = {}) {
  const dependencies = options.dependencies ?? defaultTronPaymentDependencies
  const onSuccess = options.onSuccess
  const [order, setOrder] = useState<TronTopupOrder | null>(null)
  const [open, setOpen] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [claiming, setClaiming] = useState(false)
  const successNotifiedTradeNo = useRef<string | null>(null)

  useEffect(() => {
    const tradeNo = order?.trade_no
    if (!open || !tradeNo || order.status !== 'pending') {
      return
    }

    let cancelled = false
    let scheduleID = 0
    let retryDelayMS = dependencies.pollDelayMS
    const scheduleNext = (delayMS: number) => {
      scheduleID = dependencies.schedule(poll, delayMS)
    }
    const poll = async () => {
      try {
        const response = await dependencies.getOrder(tradeNo)
        if (cancelled) {
          return
        }
        if (!isApiSuccess(response) || !response.data) {
          retryDelayMS = Math.min(retryDelayMS * 2, 30_000)
          scheduleNext(retryDelayMS)
          return
        }
        setOrder(response.data)
        if (response.data.status === 'success') {
          if (successNotifiedTradeNo.current !== tradeNo) {
            successNotifiedTradeNo.current = tradeNo
            await onSuccess?.()
          }
          return
        }
        if (response.data.status === 'pending' && !cancelled) {
          retryDelayMS = dependencies.pollDelayMS
          scheduleNext(retryDelayMS)
        }
      } catch {
        if (!cancelled) {
          retryDelayMS = Math.min(retryDelayMS * 2, 30_000)
          scheduleNext(retryDelayMS)
        }
      }
    }

    scheduleID = dependencies.schedule(poll, dependencies.pollDelayMS)
    return () => {
      cancelled = true
      dependencies.cancelSchedule(scheduleID)
    }
  }, [dependencies, onSuccess, open, order?.status, order?.trade_no])

  const startPayment = useCallback(
    async (amount: number): Promise<boolean> => {
      if (!Number.isInteger(amount) || amount <= 0) {
        toast.error(i18next.t('Invalid top-up amount'))
        return false
      }
      setProcessing(true)
      try {
        const response = await dependencies.createOrder(amount)
        if (!isApiSuccess(response) || !response.data) {
          toast.error(
            response.message || i18next.t('TRON top-up order creation failed')
          )
          return false
        }
        setOrder(response.data)
        if (response.data.trade_no !== successNotifiedTradeNo.current) {
          successNotifiedTradeNo.current = null
        }
        setOpen(true)
        return true
      } catch {
        toast.error(i18next.t('TRON top-up order creation failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    [dependencies]
  )

  const submitClaim = useCallback(
    async (txID: string, note: string): Promise<boolean> => {
      const normalizedTxID = txID.trim().toLowerCase()
      if (!order || !isValidTronTxID(normalizedTxID)) {
        toast.error(i18next.t('Enter a valid 64-character transaction ID'))
        return false
      }
      if ([...note.trim()].length > 500) {
        toast.error(i18next.t('Claim note is too long'))
        return false
      }
      setClaiming(true)
      try {
        const response = await dependencies.submitClaim({
          trade_no: order.trade_no,
          tx_id: normalizedTxID,
          note: note.trim(),
        })
        if (!isApiSuccess(response)) {
          toast.error(
            response.message || i18next.t('Top-up claim submission failed')
          )
          return false
        }
        toast.success(i18next.t('Top-up claim submitted'))
        return true
      } catch {
        toast.error(i18next.t('Top-up claim submission failed'))
        return false
      } finally {
        setClaiming(false)
      }
    },
    [dependencies, order]
  )

  const onOpenChange = useCallback((nextOpen: boolean) => {
    setOpen(nextOpen)
  }, [])

  return {
    order,
    open,
    processing,
    claiming,
    startPayment,
    submitClaim,
    onOpenChange,
  }
}
