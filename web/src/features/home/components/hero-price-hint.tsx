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
import { ArrowDown } from 'lucide-react'
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { MAIN_LINE } from '../constants'
import { useHomePricing } from '../hooks'
import { formatDiscount, getDiscountFraction } from '../lib/price-table'

interface HeroPriceHintProps {
  className?: string
  style?: CSSProperties
}

/**
 * One-line link to the price section. Shows the main line's live discount
 * once prices and the display currency are both in hand, and plain
 * "See prices" until then or when the discount cannot be stated.
 */
export function HeroPriceHint(props: HeroPriceHintProps) {
  const { t } = useTranslation()
  const pricing = useHomePricing()

  const mainLine = pricing.lines.find((line) => line.group === MAIN_LINE)
  const fraction =
    mainLine && pricing.currency
      ? getDiscountFraction(mainLine.ratio, pricing.currency)
      : null
  const text =
    mainLine && fraction !== null
      ? t('{{line}}: about {{percent}}% of the official price', {
          line: mainLine.label,
          ...formatDiscount(fraction),
        })
      : t('See prices')

  return (
    <a
      href='#pricing'
      className={cn(
        'group text-foreground/90 hover:text-foreground inline-flex h-8 items-center gap-1.5 text-sm font-medium transition-colors',
        props.className
      )}
      style={props.style}
    >
      <span className='underline decoration-blue-500/40 decoration-2 underline-offset-4'>
        {text}
      </span>
      <ArrowDown className='text-muted-foreground size-3.5 transition-transform duration-200 group-hover:translate-y-0.5' />
    </a>
  )
}
