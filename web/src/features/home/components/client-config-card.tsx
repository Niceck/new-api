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
import { ArrowRight, Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

import {
  API_BASE_URL,
  buildClientConfigs,
  type ClientConfig,
  type ClientConfigBlock,
} from '../lib/client-configs'

const CLIENT_CONFIGS = buildClientConfigs(API_BASE_URL)

interface ClientConfigCardProps {
  className?: string
}

/**
 * Hero card with copy-ready setup for the clients the site supports.
 */
export function ClientConfigCard(props: ClientConfigCardProps) {
  return (
    <div className={cn('mx-auto w-full max-w-2xl', props.className)}>
      <Tabs
        defaultValue={CLIENT_CONFIGS[0].id}
        className={cn(
          'gap-0 overflow-hidden rounded-2xl border backdrop-blur-sm',
          'border-border/60 bg-white/95 shadow-[0_20px_50px_-25px_rgba(15,23,42,0.18)]',
          'dark:border-white/[0.06] dark:bg-[#0b0f17]/95 dark:shadow-[0_20px_60px_-25px_rgba(0,0,0,0.7)]'
        )}
      >
        <TabsList
          variant='line'
          className='border-border/50 w-full justify-start gap-1 border-b px-3 py-0 group-data-horizontal/tabs:h-auto dark:border-white/[0.05]'
        >
          {CLIENT_CONFIGS.map((config) => (
            <TabsTrigger
              key={config.id}
              value={config.id}
              className='flex-none px-3 py-2.5 text-xs'
            >
              {config.label}
            </TabsTrigger>
          ))}
        </TabsList>

        {CLIENT_CONFIGS.map((config) => (
          <TabsContent key={config.id} value={config.id}>
            <ClientConfigPanel config={config} />
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}

function ClientConfigPanel(props: { config: ClientConfig }) {
  const { t } = useTranslation()

  return (
    <div className='flex min-h-[360px] flex-col'>
      <div className='flex-1 space-y-4 px-5 py-4'>
        {props.config.blocks.map((block) => (
          <ConfigBlock key={block.title} block={block} />
        ))}
      </div>
      <div className='border-border/40 bg-muted/30 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t px-5 py-3 dark:border-white/[0.05] dark:bg-white/[0.02]'>
        <p className='text-muted-foreground min-w-0 text-xs leading-relaxed'>
          {t(props.config.note)}
        </p>
        <a
          href={props.config.guideHref}
          className='text-foreground/80 hover:text-foreground group inline-flex shrink-0 items-center gap-1 text-xs font-medium transition-colors'
        >
          {t('Full setup guide')}
          <ArrowRight className='size-3 transition-transform duration-200 group-hover:translate-x-0.5' />
        </a>
      </div>
    </div>
  )
}

function ConfigBlock(props: { block: ClientConfigBlock }) {
  const { t } = useTranslation()
  const title = props.block.translateTitle
    ? t(props.block.title)
    : props.block.title

  return (
    <div>
      <div className='mb-1.5 flex items-center justify-between gap-3'>
        <span
          className={cn(
            'text-foreground/50 min-w-0 truncate text-[11px]',
            !props.block.translateTitle && 'font-mono'
          )}
        >
          {title}
        </span>
        <CopyCodeButton code={props.block.code} title={title} />
      </div>
      <pre className='border-border/40 bg-muted/30 text-foreground/85 overflow-x-auto rounded-lg border px-3.5 py-3 font-mono text-[12.5px] leading-[1.6] dark:border-white/[0.05] dark:bg-white/[0.03]'>
        <code>{props.block.code}</code>
      </pre>
    </div>
  )
}

function CopyCodeButton(props: { code: string; title: string }) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const isCopied = copiedText === props.code

  const handleCopy = async () => {
    const copied = await copyToClipboard(props.code)
    if (!copied) {
      toast.error(t('Copy failed. Select the text and copy it by hand.'))
    }
  }

  return (
    <>
      <Button
        variant='ghost'
        size='sm'
        className='text-muted-foreground hover:text-foreground h-7 shrink-0 gap-1.5 px-2 text-xs'
        onClick={handleCopy}
        aria-label={`${t('Copy')} ${props.title}`}
      >
        {isCopied ? (
          <Check className='text-success size-3.5' />
        ) : (
          <Copy className='size-3.5' />
        )}
        {isCopied ? t('Copied') : t('Copy')}
      </Button>
      {/* The button keeps its name; this is what a screen reader hears. */}
      <span role='status' className='sr-only'>
        {isCopied ? t('Copied') : ''}
      </span>
    </>
  )
}
