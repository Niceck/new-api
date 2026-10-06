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
import { KeyRound, TerminalSquare, UserPlus } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

import { SITE_PAGES } from '../../constants'

const STEP_LINK_CLASS =
  'text-foreground/80 hover:text-foreground text-xs font-medium underline decoration-border underline-offset-4 transition-colors'

interface Step {
  num: string
  title: string
  desc: string
  icon: ReactNode
  links: ReactNode
}

export function HowItWorks() {
  const { t } = useTranslation()

  const steps: Step[] = [
    {
      num: '1',
      title: t('Sign up and sign in'),
      desc: t(
        'Register with your email. A new account starts with a zero balance.'
      ),
      icon: <UserPlus className='size-6' strokeWidth={1.5} />,
      links: (
        <Link to='/sign-up' className={STEP_LINK_CLASS}>
          {t('Create account')}
        </Link>
      ),
    },
    {
      num: '2',
      title: t('Top up and create a token'),
      desc: t(
        'Top up in the wallet, then create a token in the group that matches your client.'
      ),
      icon: <KeyRound className='size-6' strokeWidth={1.5} />,
      links: (
        <>
          <Link to='/wallet' className={STEP_LINK_CLASS}>
            {t('Wallet')}
          </Link>
          <Link to='/keys' className={STEP_LINK_CLASS}>
            {t('Create a token')}
          </Link>
        </>
      ),
    },
    {
      num: '3',
      title: t('Follow the guide for your client'),
      desc: t(
        'Copy the configuration, put your token in, and run the client as usual.'
      ),
      icon: <TerminalSquare className='size-6' strokeWidth={1.5} />,
      links: (
        <>
          <a href={SITE_PAGES.claudeCode} className={STEP_LINK_CLASS}>
            Claude Code
          </a>
          <a href={SITE_PAGES.codex} className={STEP_LINK_CLASS}>
            Codex CLI
          </a>
          <a href={SITE_PAGES.docs} className={STEP_LINK_CLASS}>
            {t('All setup docs')}
          </a>
        </>
      ),
    },
  ]

  return (
    <section className='border-border/40 relative z-10 border-t px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-12 text-center'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('Getting started')}
          </p>
          <h2 className='text-2xl font-bold tracking-tight md:text-3xl'>
            {t('Three steps to get started')}
          </h2>
        </AnimateInView>

        <div className='grid gap-10 md:grid-cols-3 md:gap-12'>
          {steps.map((step, i) => (
            <AnimateInView
              key={step.num}
              delay={i * 150}
              animation='fade-up'
              className='relative flex flex-col items-center text-center'
            >
              <div className='relative mb-6'>
                <div className='text-muted-foreground border-border/50 bg-muted/30 flex size-16 items-center justify-center rounded-2xl border transition-colors'>
                  {step.icon}
                </div>
                <div className='bg-foreground text-background absolute -top-2 -right-2 flex size-6 items-center justify-center rounded-full text-xs font-bold'>
                  {step.num}
                </div>
              </div>
              <h3 className='mb-2 text-base font-semibold'>{step.title}</h3>
              <p className='text-muted-foreground max-w-[260px] text-sm leading-relaxed'>
                {step.desc}
              </p>
              <div className='mt-4 flex flex-wrap items-center justify-center gap-x-4 gap-y-2'>
                {step.links}
              </div>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
