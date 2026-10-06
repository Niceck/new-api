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
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { Skeleton } from '@/components/ui/skeleton'

import { SITE_PAGES } from '../../constants'
import { useHomePricing } from '../../hooks'
import {
  getDiscountFraction,
  getSharedInputLimit,
  USD_CNY_ROUGH,
} from '../../lib/price-table'
import { PriceLineCard } from '../price-line-card'

const SKELETON_CARDS = ['a', 'b', 'c']

export function Pricing() {
  const { t } = useTranslation()
  const pricing = useHomePricing()
  const currency = pricing.currency
  const hasLimitedQuote = pricing.lines.some((line) =>
    line.rows.some((row) => row.quote !== null && row.quote.inputLimit !== null)
  )
  const sharedInputLimit = getSharedInputLimit(pricing.lines)
  const hasRoughDiscount =
    currency !== null &&
    currency.quotaDisplayType === 'CNY' &&
    pricing.lines.some(
      (line) => getDiscountFraction(line.ratio, currency) !== null
    )

  return (
    <section
      id='pricing'
      className='border-border/40 relative z-10 scroll-mt-16 border-t px-6 py-16 md:py-24'
    >
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-10 max-w-2xl'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('Pricing')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
            {t('Official USD price × group multiplier = what you pay')}
          </h2>
          <p className='text-muted-foreground mt-4 text-sm leading-relaxed md:text-base'>
            {t(
              'Top up 1 yuan for 1 yuan of credit and pay only for the tokens you use. No subscription, no currency conversion on your side.'
            )}
          </p>
        </AnimateInView>

        {pricing.isLoading && <PricingSkeleton />}
        {pricing.isUnavailable && <PricingUnavailable />}
        {currency !== null && (
          <div className='grid gap-4 md:grid-cols-2 lg:grid-cols-3'>
            {pricing.lines.map((line, i) => (
              <AnimateInView key={line.group} delay={i * 100}>
                <PriceLineCard line={line} currency={currency} />
              </AnimateInView>
            ))}
          </div>
        )}

        <div className='text-muted-foreground mt-6 space-y-1.5 text-xs leading-relaxed'>
          {pricing.lines.length > 0 && (
            <p>{t('Prices are per million tokens, input / output.')}</p>
          )}
          {hasRoughDiscount && (
            <p>
              {t(
                'Discounts are a rough figure at 1 USD ≈ {{rate}} CNY. What you are charged depends only on the multiplier.',
                { rate: USD_CNY_ROUGH }
              )}
            </p>
          )}
          {hasLimitedQuote && (
            <p>
              {sharedInputLimit === null
                ? t(
                    '* Price for requests within the standard input length. Longer requests are charged in full at the long-context rate; see the price table.'
                  )
                : t(
                    '* Price for requests with up to {{limit}} input tokens. Longer requests are charged in full at the long-context rate; see the price table.',
                    { limit: sharedInputLimit.toLocaleString('en-US') }
                  )}
            </p>
          )}
        </div>

        <div className='mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm font-medium'>
          <a
            href={SITE_PAGES.priceTable}
            className='group hover:text-foreground text-foreground/80 inline-flex items-center gap-1 transition-colors'
          >
            {t('Full price table, with cache pricing')}
            <ArrowRight className='size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
          </a>
          <Link
            to='/pricing'
            className='group hover:text-foreground text-foreground/80 inline-flex items-center gap-1 transition-colors'
          >
            {t('Model Square')}
            <ArrowRight className='size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
          </Link>
        </div>
      </div>
    </section>
  )
}

function PricingSkeleton() {
  return (
    <div className='grid gap-4 md:grid-cols-2 lg:grid-cols-3' aria-hidden>
      {SKELETON_CARDS.map((key) => (
        <div key={key} className='border-border/50 rounded-xl border p-6'>
          <Skeleton className='h-4 w-28' />
          <Skeleton className='mt-4 h-7 w-44' />
          <Skeleton className='mt-6 h-24 w-full' />
        </div>
      ))}
    </div>
  )
}

function PricingUnavailable() {
  const { t } = useTranslation()

  return (
    <div className='border-border/50 bg-muted/20 text-muted-foreground rounded-xl border p-6 text-sm leading-relaxed'>
      {t(
        'Live prices could not be loaded just now. The full price table below has every model and line.'
      )}
    </div>
  )
}
