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
import { useTranslation } from 'react-i18next'

import { formatBasePriceUSD } from '@/features/pricing/lib/price'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { cn } from '@/lib/utils'
import type { CurrencyConfig } from '@/stores/system-config-store'

import { LINE_NOTES, MAIN_LINE } from '../constants'
import {
  formatDiscount,
  getDiscountFraction,
  type PriceLine,
  type PriceQuote,
  type PriceRow,
} from '../lib/price-table'

const SITE_PRICE_FORMAT = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }

interface PriceLineCardProps {
  line: PriceLine
  currency: CurrencyConfig
}

/**
 * One line (group): its discount, multiplier and headline model prices.
 */
export function PriceLineCard(props: PriceLineCardProps) {
  const { t } = useTranslation()
  const fraction = getDiscountFraction(props.line.ratio, props.currency)
  const note = LINE_NOTES[props.line.group]
  const isMain = props.line.group === MAIN_LINE
  const multiplier = t('Multiplier {{ratio}}', {
    ratio: props.line.ratio.toFixed(2),
  })

  return (
    <div
      className={cn(
        'bg-background flex h-full flex-col rounded-xl border p-6',
        isMain ? 'border-blue-500/40 shadow-sm' : 'border-border/50'
      )}
    >
      <div className='flex items-center gap-2'>
        <h3 className='text-sm font-semibold'>{props.line.label}</h3>
        {isMain && (
          <span className='rounded-full bg-blue-500/10 px-2 py-0.5 text-[10px] font-medium text-blue-600 dark:bg-blue-400/10 dark:text-blue-400'>
            {t('Main line')}
          </span>
        )}
      </div>

      <p className='mt-3 text-xl font-bold tracking-tight'>
        {fraction === null
          ? multiplier
          : t(
              'About {{percent}}% of the official price',
              formatDiscount(fraction)
            )}
      </p>
      {fraction !== null && (
        <p className='text-muted-foreground mt-1 text-xs'>{multiplier}</p>
      )}
      {note && (
        <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
          {t(note)}
        </p>
      )}

      <table className='mt-5 w-full text-xs'>
        <caption className='sr-only'>{props.line.label}</caption>
        <thead>
          <tr className='text-muted-foreground border-border/40 border-b text-left'>
            <th scope='col' className='pb-2 font-medium'>
              {t('Model')}
            </th>
            <th scope='col' className='pb-2 text-right font-medium'>
              {t('Official price')}
            </th>
            <th scope='col' className='pb-2 text-right font-medium'>
              {t('Price here')}
            </th>
          </tr>
        </thead>
        <tbody>
          {props.line.rows.map((row) => (
            <PriceRowView key={row.model} row={row} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function formatQuote(quote: PriceQuote): { official: string; site: string } {
  return {
    official: `${formatBasePriceUSD(quote.officialInput)} / ${formatBasePriceUSD(quote.officialOutput)}`,
    site: `${formatBillingCurrencyFromUSD(quote.siteInput, SITE_PRICE_FORMAT)} / ${formatBillingCurrencyFromUSD(quote.siteOutput, SITE_PRICE_FORMAT)}`,
  }
}

function PriceRowView(props: { row: PriceRow }) {
  const { t } = useTranslation()
  const quote = props.row.quote
  const prices = quote === null ? null : formatQuote(quote)

  return (
    <tr className='border-border/30 border-b last:border-b-0'>
      <th
        scope='row'
        className='py-2.5 pr-2 text-left font-mono text-[11.5px] font-normal'
      >
        {props.row.model}
        {quote !== null && quote.inputLimit !== null && (
          <span aria-hidden> *</span>
        )}
      </th>
      {prices === null ? (
        <td colSpan={2} className='text-muted-foreground py-2.5 text-right'>
          {t('See the price table for how this model is priced')}
        </td>
      ) : (
        <>
          <td className='text-muted-foreground py-2.5 pr-2 text-right whitespace-nowrap tabular-nums'>
            {prices.official}
          </td>
          <td className='py-2.5 text-right font-semibold whitespace-nowrap tabular-nums'>
            {prices.site}
          </td>
        </>
      )}
    </tr>
  )
}
