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

import { formatQuotaWithCurrency } from '@/lib/currency'

import { formatTronUSDTAmount } from '../../lib/tron'
import type { TronTopupOrder } from '../../types'

/** What happens to an amount that is not exact, stated before the user pays. */
export function TronToleranceRules(props: { order: TronTopupOrder }) {
  const { t } = useTranslation()
  return (
    <ul className='text-muted-foreground mt-2 list-disc space-y-1 pl-4 text-xs leading-5'>
      <li>
        {t(
          'An amount that is not exact but between {{min}} and {{max}} USDT is credited automatically only if it arrives at least 60 seconds after this order was created, before the countdown ends, and matches this order alone',
          {
            min: formatTronUSDTAmount(
              props.order.tolerant_min_usdt_micros ?? 0
            ),
            max: formatTronUSDTAmount(
              props.order.tolerant_max_usdt_micros ?? 0
            ),
          }
        )}
      </li>
      <li>{t('Paying more adds no credit and is not refunded')}</li>
      <li>
        {t(
          'Paying less is credited in proportion and settles this order — create a new order to top up the difference'
        )}
      </li>
      <li>
        {t(
          'Anything else is reviewed manually, as are orders created after several others within 24 hours — submit a claim with the transaction ID'
        )}
      </li>
    </ul>
  )
}

/** After settlement: what arrived and what was credited, when they differ from the quote. */
export function TronSettlementSummary(props: { order: TronTopupOrder }) {
  const { t } = useTranslation()
  const received = props.order.received_usdt_micros ?? 0
  const expected = props.order.expected_usdt_micros
  const credited = props.order.credited_quota ?? 0
  const face = props.order.credit_quota
  if (
    props.order.status !== 'success' ||
    received <= 0 ||
    (received === expected && credited === face)
  ) {
    return null
  }
  // The note follows what was actually credited, not today's rules: an order
  // settled before overpayments were capped may show more than its face value,
  // and then only the facts are shown.
  let note: string | null = null
  if (credited > 0 && credited < face) {
    note = t(
      'Paid less than the amount due, so it was credited in proportion and this order is settled'
    )
  } else if (credited === face && received > expected) {
    note = t(
      'Paid more than the amount due, so the face value of the order was credited'
    )
  }
  return (
    <div className='rounded-lg border p-3 text-sm'>
      <dl className='grid grid-cols-2 gap-2'>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Amount received')}
          </dt>
          <dd className='mt-1 font-mono font-medium tabular-nums'>
            {formatTronUSDTAmount(received)} USDT
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Amount credited')}
          </dt>
          <dd className='mt-1 font-medium tabular-nums'>
            {formatQuotaWithCurrency(credited)}
          </dd>
        </div>
      </dl>
      {note ? (
        <p className='text-muted-foreground mt-2 text-xs leading-5'>{note}</p>
      ) : null}
    </div>
  )
}
