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
import { Check, CreditCard, Layers, ListChecks, Route } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

const MODEL_NAMES = [
  'Opus 5.5',
  'Sonnet 5.5',
  'Fable 5.1',
  'GPT-6 Astra',
  'GPT-6 Sol',
  'GPT-6 Luna',
]

interface FeatureCard {
  id: string
  title: string
  desc: string
  span: string
  icon: ReactNode
  visual: ReactNode
}

function CheckList(props: { items: string[] }) {
  return (
    <ul className='mt-4 space-y-2'>
      {props.items.map((item) => (
        <li
          key={item}
          className='text-muted-foreground flex items-start gap-2 text-xs leading-relaxed'
        >
          <Check className='mt-0.5 size-3.5 shrink-0 text-emerald-500' />
          <span>{item}</span>
        </li>
      ))}
    </ul>
  )
}

function ClientLines(props: { rows: { client: string; line: string }[] }) {
  return (
    <dl className='mt-4 space-y-2'>
      {props.rows.map((row) => (
        <div key={row.client} className='flex items-center gap-2 text-xs'>
          <dt className='font-medium'>{row.client}</dt>
          <div aria-hidden className='bg-border/60 h-px flex-1' />
          <dd className='text-muted-foreground'>{row.line}</dd>
        </div>
      ))}
    </dl>
  )
}

function ChipRow(props: { items: string[]; className?: string }) {
  return (
    <div className={props.className}>
      {props.items.map((item) => (
        <div
          key={item}
          className='border-border/30 bg-muted/20 text-muted-foreground flex items-center justify-center rounded-lg border px-3 py-2 text-xs'
        >
          {item}
        </div>
      ))}
    </div>
  )
}

export function Features() {
  const { t } = useTranslation()

  const features: FeatureCard[] = [
    {
      id: 'models',
      title: t('Models on sale'),
      desc: t(
        'Claude Opus 5.5, Sonnet 5.5 and Fable 5.1, plus GPT-6 Astra, Sol and Luna. The model catalog has the full list.'
      ),
      span: 'md:col-span-2',
      icon: <Layers className='size-4 text-blue-400' />,
      visual: (
        <ChipRow
          items={MODEL_NAMES}
          className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3'
        />
      ),
    },
    {
      id: 'billing',
      title: t('Pay for what you use'),
      desc: t(
        'Charged by the tokens actually used, thinking tokens included. No subscription.'
      ),
      span: 'md:col-span-1',
      icon: <CreditCard className='size-4 text-emerald-400' />,
      visual: (
        <CheckList
          items={[
            t('1 yuan topped up = 1 yuan of credit'),
            t('Alipay, or TRC20-USDT from ¥100'),
          ]}
        />
      ),
    },
    {
      id: 'lines',
      title: t('One token, one line'),
      desc: t(
        'A token belongs to one group and cannot call models on another line. Create one per client.'
      ),
      span: 'md:col-span-1',
      icon: <Route className='size-4 text-violet-400' />,
      visual: (
        <ClientLines
          rows={[
            { client: 'Claude Code', line: t('Claude lines') },
            { client: 'Codex CLI', line: t('GPT-6 line') },
            { client: 'OpenAI Chat', line: t('GPT-6 line') },
          ]}
        />
      ),
    },
    {
      id: 'usage',
      title: t('Every request is itemised'),
      desc: t(
        'The console lists each request with its model, token counts and the amount charged, next to your balance.'
      ),
      span: 'md:col-span-2',
      icon: <ListChecks className='size-4 text-amber-400' />,
      visual: (
        <ChipRow
          items={[
            t('Model'),
            t('Input and output tokens'),
            t('Amount charged'),
            t('Balance'),
          ]}
          className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4'
        />
      ),
    },
  ]

  return (
    <section className='border-border/40 relative z-10 border-t px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-10 max-w-lg'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('What you get')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
            {t('Models, billing and lines at a glance')}
          </h2>
        </AnimateInView>

        {/* Bento grid */}
        <div className='border-border/40 bg-border/40 grid gap-px overflow-hidden rounded-xl border md:grid-cols-3'>
          {features.map((f, i) => (
            <AnimateInView
              key={f.id}
              delay={i * 100}
              animation='scale-in'
              className={`bg-background hover:bg-muted/20 p-7 transition-colors duration-300 md:p-8 ${f.span}`}
            >
              <div className='mb-3 flex items-center gap-3'>
                <span className='border-border/40 bg-muted flex size-7 items-center justify-center rounded-md border'>
                  {f.icon}
                </span>
                <h3 className='text-sm font-semibold'>{f.title}</h3>
              </div>
              <p className='text-muted-foreground text-sm leading-relaxed'>
                {f.desc}
              </p>
              {f.visual}
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
