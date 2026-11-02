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
import { useState, useCallback } from 'react'
import { toast } from 'sonner'

import {
  calculateAmount,
  calculateStripeAmount,
  calculateWaffoAmount,
  calculateWaffoPancakeAmount,
  requestPayment,
  requestStripePayment,
  isApiSuccess,
} from '../api'
import {
  isStripePayment,
  isWaffoPayment,
  isWaffoPancakePayment,
  submitPaymentForm,
} from '../lib'
import type { AmountRequest, AmountResponse } from '../types'

// ============================================================================
// Payment Hook
// ============================================================================

type AmountCalculator = (request: AmountRequest) => Promise<AmountResponse>

export interface PaymentAmountCalculators {
  regular: AmountCalculator
  stripe: AmountCalculator
  waffo: AmountCalculator
  waffoPancake: AmountCalculator
}

const defaultPaymentAmountCalculators: PaymentAmountCalculators = {
  regular: calculateAmount,
  stripe: calculateStripeAmount,
  waffo: calculateWaffoAmount,
  waffoPancake: calculateWaffoPancakeAmount,
}

/** What the server quoted: money to pay and, when there is one, the bonus. */
export interface PaymentQuote {
  money: number
  /** The money exactly as the server wrote it; sent back when confirming */
  moneyText: string
  bonusRate: number
  bonusAmount: number
  bonusQuota: number
  creditAmount: number
  /** Which top-up amount this quote is for */
  quotedFor: number
}

const emptyPaymentQuote: PaymentQuote = {
  money: 0,
  moneyText: '',
  bonusRate: 0,
  bonusAmount: 0,
  bonusQuota: 0,
  creditAmount: 0,
  quotedFor: 0,
}

/**
 * Quotes can come back out of order (typing 2000 then 600). Each call gets a
 * ticket; only the newest ticket may publish its result, so a late answer for
 * an older amount can never show that amount's bonus next to the new one.
 */
export function latestOnly(): () => () => boolean {
  let latest = 0
  return () => {
    latest += 1
    const ticket = latest
    return () => ticket === latest
  }
}

/** A failed order answers {message:'error', data:'<the reason>'}: show the reason. */
export function paymentErrorMessage(response: {
  message?: string
  data?: unknown
}): string | undefined {
  if (response.message === 'error' && typeof response.data === 'string') {
    return response.data
  }
  return response.message
}

export async function requestPaymentQuote(
  topupAmount: number,
  paymentType: string,
  calculators: PaymentAmountCalculators = defaultPaymentAmountCalculators
): Promise<PaymentQuote> {
  let calculator = calculators.regular
  if (isStripePayment(paymentType)) {
    calculator = calculators.stripe
  } else if (isWaffoPayment(paymentType)) {
    calculator = calculators.waffo
  } else if (isWaffoPancakePayment(paymentType)) {
    calculator = calculators.waffoPancake
  }

  const response = await calculator({ amount: topupAmount })
  if (!isApiSuccess(response) || !response.data) {
    return { ...emptyPaymentQuote, quotedFor: topupAmount }
  }

  return {
    money: Number.parseFloat(response.data),
    moneyText: response.data,
    bonusRate: response.bonus_rate ?? 0,
    bonusAmount: response.bonus_amount ?? 0,
    bonusQuota: response.bonus_quota ?? 0,
    creditAmount: response.credit_amount ?? 0,
    quotedFor: topupAmount,
  }
}

export async function requestPaymentAmount(
  topupAmount: number,
  paymentType: string,
  calculators: PaymentAmountCalculators = defaultPaymentAmountCalculators
): Promise<number> {
  const quote = await requestPaymentQuote(topupAmount, paymentType, calculators)
  return quote.money
}

export function usePayment() {
  const [quote, setQuote] = useState<PaymentQuote>(emptyPaymentQuote)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [nextQuoteTicket] = useState(latestOnly)

  // Calculate payment amount
  const calculatePaymentAmount = useCallback(
    async (topupAmount: number, paymentType: string) => {
      const isLatest = nextQuoteTicket()
      try {
        setCalculating(true)
        const calculated = await requestPaymentQuote(topupAmount, paymentType)
        if (isLatest()) {
          setQuote(calculated)
        }
        return calculated.money
      } catch {
        if (isLatest()) {
          setQuote(emptyPaymentQuote)
        }
        return 0
      } finally {
        if (isLatest()) {
          setCalculating(false)
        }
      }
    },
    [nextQuoteTicket]
  )

  // Process payment
  const processPayment = useCallback(
    async (
      topupAmount: number,
      paymentType: string,
      confirmed?: { money: string; bonusQuota: number }
    ) => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const amount = Math.floor(topupAmount)

        const response = isStripe
          ? await requestStripePayment({
              amount,
              payment_method: 'stripe',
            })
          : await requestPayment({
              amount,
              payment_method: paymentType,
              ...(confirmed
                ? {
                    expected_money: confirmed.money,
                    expected_bonus_quota: confirmed.bonusQuota,
                  }
                : {}),
            })

        if (!isApiSuccess(response)) {
          toast.error(
            paymentErrorMessage(response) || i18next.t('Payment request failed')
          )
          return false
        }

        // Handle Stripe payment
        if (isStripe && response.data?.pay_link) {
          window.open(response.data.pay_link as string, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return true
        }

        // Handle non-Stripe payment
        if (!isStripe && response.data) {
          const url = (response as unknown as { url?: string }).url
          if (url) {
            submitPaymentForm(url, response.data)
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
        }

        return false
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  const setAmount = useCallback((money: number) => {
    setQuote({ ...emptyPaymentQuote, money })
  }, [])

  return {
    amount: quote.money,
    moneyText: quote.moneyText,
    bonusRate: quote.bonusRate,
    bonusAmount: quote.bonusAmount,
    bonusQuota: quote.bonusQuota,
    creditAmount: quote.creditAmount,
    quotedFor: quote.quotedFor,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    setAmount,
  }
}
