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
import { Crown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Progress } from '@/components/ui/progress'

import {
  bonusPercent,
  formatCurrency,
  getDiscountParts,
  isBonusTier,
  tierProgress,
} from '../lib'
import type { TopupTierStatus } from '../types'

interface TopupTierBannerProps {
  tier: TopupTierStatus
  usdExchangeRate?: number
}

/**
 * Member tier block at the top of the recharge card: the current level and
 * its rate, how much has been topped up so far, and the road to the next
 * level. Amounts are shown the same way the preset cards show theirs.
 */
export function TopupTierBanner({
  tier,
  usdExchangeRate = 1,
}: TopupTierBannerProps) {
  const { t } = useTranslation()
  const display = (units: number) => formatCurrency(units * usdExchangeRate)
  const current = tier.level > 0 ? getDiscountParts(tier.discount) : null
  const hasNext =
    tier.next_level !== undefined && tier.next_threshold !== undefined
  const next =
    tier.next_discount !== undefined
      ? getDiscountParts(tier.next_discount)
      : null
  const firstThreshold = tier.tiers[0]?.threshold ?? tier.next_threshold ?? 0
  const remaining =
    tier.remaining ??
    (tier.next_threshold !== undefined
      ? Math.max(tier.next_threshold - tier.total, 0)
      : 0)

  const bonusMode = isBonusTier(tier)

  let headline = t('Member level')
  if (bonusMode && tier.level > 0 && tier.bonus) {
    headline = t('Every top-up gets {{percent}}% extra credit', {
      percent: bonusPercent(tier.bonus),
    })
  } else if (bonusMode && tier.level === 0) {
    headline = t(
      'Top up {{amount}} in total to unlock permanent bonus credit',
      {
        amount: display(firstThreshold),
      }
    )
  } else if (tier.level > 0 && current) {
    headline = t('Every top-up at {{percent}}% off', current)
  } else if (tier.level === 0) {
    headline = t('Top up {{amount}} in total to unlock a permanent discount', {
      amount: display(firstThreshold),
    })
  }

  let nextLine = t('Top up {{amount}} more to reach VIP{{level}}', {
    amount: display(remaining),
    level: tier.next_level,
  })
  if (bonusMode && tier.next_bonus) {
    nextLine = t(
      'Top up {{amount}} more to reach VIP{{level}} ({{percent}}% extra credit)',
      {
        amount: display(remaining),
        level: tier.next_level,
        percent: bonusPercent(tier.next_bonus),
      }
    )
  } else if (!bonusMode && next) {
    nextLine = t(
      'Top up {{amount}} more to reach VIP{{level}} ({{percent}}% off)',
      {
        amount: display(remaining),
        level: tier.next_level,
        ...next,
      }
    )
  }

  return (
    <div
      data-testid='topup-tier'
      className='bg-muted/30 space-y-2 rounded-lg border px-3 py-2.5 sm:px-4 sm:py-3'
    >
      <div className='flex flex-wrap items-center gap-x-3 gap-y-1'>
        <span className='inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold'>
          <Crown className='h-3.5 w-3.5' aria-hidden='true' />
          VIP{tier.level}
        </span>
        <span className='text-sm font-medium'>{headline}</span>
        <span className='text-muted-foreground text-xs'>
          {t('Total topped up {{amount}}', { amount: display(tier.total) })}
        </span>
      </div>
      {hasNext ? (
        <div className='space-y-1'>
          <Progress
            value={tierProgress(tier)}
            aria-label={t('Progress to VIP{{level}}', {
              level: tier.next_level,
            })}
          />
          <p className='text-muted-foreground text-xs'>{nextLine}</p>
        </div>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {t('Highest level reached')}
        </p>
      )}
      <p className='text-muted-foreground text-[11px] leading-4'>
        {bonusMode
          ? t(
              'Your level bonus applies to every top-up; when a single-order bonus is higher, you get that one instead'
            )
          : t(
              'Your level discount applies to every top-up; when a single-order discount is better, you get that one instead'
            )}
      </p>
    </div>
  )
}
