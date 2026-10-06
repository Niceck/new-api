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
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'

import { SITE_PAGES, SUPPORT_QQ_GROUP } from '../../constants'
import { API_BASE_URL } from '../../lib/client-configs'

export function Faq() {
  const { t } = useTranslation()

  // Kept to answers that do not depend on prices; the static FAQ page has
  // the rest and follows the price snapshot.
  const items = [
    {
      id: 'official',
      question: t('How is CC API different from the official API?'),
      answer: t(
        'CC API is a relay: it forwards requests from clients such as the official Claude Code and Codex CLI to upstream models and bills usage in yuan. You change the API address to {{apiBase}} and use a token created here. The service runs on third-party upstreams and does not promise an SLA.',
        { apiBase: API_BASE_URL }
      ),
    },
    {
      id: 'claude-clients',
      question: t(
        'Can I call Claude models from other clients or my own code?'
      ),
      answer: t(
        'The Claude lines are only guaranteed for the official Claude Code client. Raw curl requests and third-party SDKs may be rejected upstream. Clients on the OpenAI Chat protocol should use the GPT-6 line.'
      ),
    },
    {
      id: 'topup',
      question: t('How do I top up, and how is credit counted?'),
      answer: t(
        'Top up on the wallet page of the console: 1 yuan topped up is 1 yuan of credit. Alipay is supported, and so is TRC20-USDT with a minimum of ¥100.'
      ),
    },
    {
      id: 'refund',
      question: t('Can I get a refund?'),
      answer: t(
        'Billing follows the tokens actually generated, thinking tokens included. Requests that returned successfully are not refunded; please keep this in mind before topping up.'
      ),
    },
    {
      id: 'sla',
      question: t('Does CC API promise an SLA?'),
      answer: t(
        'No. The service runs on third-party upstreams and does not promise an SLA.'
      ),
    },
  ]

  return (
    <section className='border-border/40 relative z-10 border-t px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-3xl'>
        <AnimateInView className='mb-8 text-center'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('FAQ')}
          </p>
          <h2 className='text-2xl font-bold tracking-tight md:text-3xl'>
            {t('Before you top up')}
          </h2>
        </AnimateInView>

        <AnimateInView>
          <Accordion className='border-border/50 rounded-xl border px-5'>
            {items.map((item) => (
              <AccordionItem key={item.id} value={item.id}>
                <AccordionTrigger className='py-4 text-sm'>
                  {item.question}
                </AccordionTrigger>
                <AccordionContent className='text-muted-foreground pb-4 leading-relaxed'>
                  {item.answer}
                </AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>

          <div className='text-muted-foreground mt-6 flex flex-wrap items-center justify-between gap-x-6 gap-y-2 text-sm'>
            <p>
              {t('Setup problems? Join QQ group {{group}}.', {
                group: SUPPORT_QQ_GROUP,
              })}
            </p>
            <a
              href={SITE_PAGES.faq}
              className='group text-foreground/80 hover:text-foreground inline-flex items-center gap-1 font-medium transition-colors'
            >
              {t('All questions')}
              <ArrowRight className='size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
            </a>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
