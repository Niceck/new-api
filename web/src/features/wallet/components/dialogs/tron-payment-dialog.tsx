import { QRCodeSVG } from 'qrcode.react'
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
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import {
  formatTronCNYValue,
  formatTronRemaining,
  formatTronUSDTAmount,
  isValidTronTxID,
} from '../../lib/tron'
import type { TronTopupOrder } from '../../types'

interface TronPaymentDialogProps {
  open: boolean
  order: TronTopupOrder | null
  onOpenChange: (open: boolean) => void
  onSubmitClaim: (txID: string, note: string) => Promise<boolean>
  claiming: boolean
}

function formatRate(micros: number): string {
  return formatTronUSDTAmount(micros)
}

export function TronPaymentDialog(props: TronPaymentDialogProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const [nowMS, setNowMS] = useState(() => Date.now())
  const [showClaim, setShowClaim] = useState(false)
  const [txID, setTxID] = useState('')
  const [note, setNote] = useState('')
  const [claimSubmitted, setClaimSubmitted] = useState(false)

  const tradeNo = props.order?.trade_no
  const orderStatus = props.order?.status
  useEffect(() => {
    setNowMS(Date.now())
    setShowClaim(false)
    setTxID('')
    setNote('')
    setClaimSubmitted(false)
  }, [tradeNo])

  useEffect(() => {
    if (props.open) {
      return
    }
    setShowClaim(false)
    setTxID('')
    setNote('')
    setClaimSubmitted(false)
  }, [props.open])

  useEffect(() => {
    if (!props.open || orderStatus !== 'pending') {
      return
    }
    const timer = window.setInterval(() => setNowMS(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [orderStatus, props.open])

  if (!props.order) {
    return null
  }

  const amount = formatTronUSDTAmount(props.order.expected_usdt_micros)
  const tokenContract = props.order.token_contract
  const visibleNowMS = props.open ? Date.now() : nowMS
  const expired =
    props.order.status === 'expired' || visibleNowMS > props.order.expires_at_ms
  const canClaim = expired || showClaim
  const remaining = formatTronRemaining(
    props.order.expires_at_ms - visibleNowMS
  )
  const cnyValue = formatTronCNYValue(
    props.order.expected_usdt_micros,
    props.order.rate_cny_micros
  )

  const handleClaim = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const success = await props.onSubmitClaim(txID, note)
    if (success) {
      setClaimSubmitted(true)
    }
  }

  let statusContent: ReactNode = (
    <span className='font-mono font-semibold tabular-nums'>{remaining}</span>
  )
  if (props.order.status === 'success') {
    statusContent = (
      <span className='font-medium text-green-600'>{t('Credited')}</span>
    )
  } else if (expired) {
    statusContent = (
      <span className='font-medium text-amber-600'>{t('Expired')}</span>
    )
  }

  let recoveryContent: ReactNode = null
  if (props.order.status !== 'success') {
    if (canClaim) {
      recoveryContent = (
        <form className='space-y-3 border-t pt-4' onSubmit={handleClaim}>
          <div className='space-y-2'>
            <Label htmlFor='tron-claim-txid'>{t('Transaction ID')}</Label>
            <Input
              id='tron-claim-txid'
              inputMode='text'
              autoComplete='off'
              spellCheck={false}
              maxLength={64}
              value={txID}
              onChange={(event) => setTxID(event.target.value)}
              aria-invalid={txID.length > 0 && !isValidTronTxID(txID)}
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='tron-claim-note'>{t('Claim note')}</Label>
            <Input
              id='tron-claim-note'
              maxLength={500}
              value={note}
              onChange={(event) => setNote(event.target.value)}
              placeholder={t('For example: the exchange deducted a fee')}
            />
          </div>
          {claimSubmitted ? (
            <Alert>
              <AlertDescription>
                {t('Your claim was submitted for manual review.')}
              </AlertDescription>
            </Alert>
          ) : null}
          <Button type='submit' disabled={props.claiming} className='w-full'>
            {props.claiming ? t('Submitting...') : t('Submit top-up claim')}
          </Button>
        </form>
      )
    } else {
      recoveryContent = (
        <Button
          type='button'
          variant='link'
          className='h-auto w-full whitespace-normal'
          onClick={() => setShowClaim(true)}
        >
          {t('Payment sent but not credited? Submit a claim')}
        </Button>
      )
    }
  }

  let statusAnnouncement = ''
  if (props.order.status === 'success') {
    statusAnnouncement = t('Top-up credited successfully')
  } else if (expired) {
    statusAnnouncement = t('Top-up order expired')
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100vh-2rem)] overflow-y-auto overscroll-contain sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Pay with USDT on TRON')}</DialogTitle>
          <DialogDescription>
            {t('Send the exact amount before the countdown ends.')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <Alert variant='destructive'>
            <AlertTitle>{t('TRON mainnet only')}</AlertTitle>
            <AlertDescription>
              {t(
                'Only send official USDT using TRON / TRC20. Other networks or tokens cannot be credited automatically.'
              )}
            </AlertDescription>
          </Alert>

          <div
            className='flex flex-col items-center gap-3 rounded-xl border bg-white p-4 text-slate-950 dark:bg-white'
            aria-label={t('TRON payment address QR code')}
          >
            <QRCodeSVG
              value={props.order.receive_address}
              size={176}
              level='M'
              marginSize={1}
              role='img'
              aria-label={t('TRON payment address QR code')}
            />
            <span className='text-xs font-medium text-slate-700'>
              {t('Scan the receiving address')}
            </span>
          </div>

          <div className='grid gap-3'>
            <div className='rounded-lg border p-3'>
              <div className='text-muted-foreground text-xs font-medium uppercase'>
                {t('Exact amount')}
              </div>
              <div className='mt-1 flex min-w-0 items-center justify-between gap-2'>
                <span className='font-mono text-lg font-semibold tabular-nums'>
                  {amount} USDT
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  aria-label={t('Copy payment amount')}
                  onClick={() => void copyToClipboard(amount)}
                >
                  {t('Copy')}
                </Button>
              </div>
            </div>

            <div className='rounded-lg border p-3'>
              <div className='text-muted-foreground text-xs font-medium uppercase'>
                {t('Receiving address')}
              </div>
              <div className='mt-1 flex min-w-0 items-start justify-between gap-2'>
                <span
                  data-tron-address='true'
                  className='min-w-0 font-mono text-sm leading-5 break-all'
                >
                  {props.order.receive_address}
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  aria-label={t('Copy receiving address')}
                  onClick={() =>
                    void copyToClipboard(props.order?.receive_address ?? '')
                  }
                >
                  {t('Copy')}
                </Button>
              </div>
            </div>

            <div className='rounded-lg border p-3'>
              <div className='text-muted-foreground text-xs font-medium uppercase'>
                {t('Official token contract')}
              </div>
              <div className='mt-1 flex min-w-0 items-start justify-between gap-2'>
                <span className='min-w-0 font-mono text-sm leading-5 break-all'>
                  {tokenContract}
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  aria-label={t('Copy token contract')}
                  onClick={() => void copyToClipboard(tokenContract)}
                >
                  {t('Copy')}
                </Button>
              </div>
            </div>

            <dl className='grid grid-cols-2 gap-2 text-sm'>
              <div className='bg-muted/50 rounded-lg p-3'>
                <dt className='text-muted-foreground'>{t('Network')}</dt>
                <dd className='mt-1 font-medium'>TRON / TRC20</dd>
              </div>
              <div className='bg-muted/50 rounded-lg p-3'>
                <dt className='text-muted-foreground'>{t('Locked rate')}</dt>
                <dd className='mt-1'>
                  <div className='font-medium tabular-nums'>
                    ¥{formatRate(props.order.rate_cny_micros)} / USDT
                  </div>
                  <a
                    href='https://www.coingecko.com/en/api'
                    target='_blank'
                    rel='noopener noreferrer'
                    className='text-muted-foreground mt-1 inline-block text-xs underline underline-offset-2'
                  >
                    {t('Price data by CoinGecko')}
                  </a>
                </dd>
              </div>
              <div className='bg-muted/50 col-span-2 rounded-lg p-3'>
                <dt className='text-muted-foreground'>
                  {t('Payment value at locked rate')}
                </dt>
                <dd className='mt-1 font-medium tabular-nums'>¥{cnyValue}</dd>
              </div>
            </dl>
          </div>

          <div className='flex items-center justify-between rounded-lg border px-3 py-2 text-sm'>
            <span className='text-muted-foreground'>{t('Payment status')}</span>
            {statusContent}
          </div>
          <span className='sr-only' aria-live='polite'>
            {statusAnnouncement}
          </span>
          {recoveryContent}
        </div>
      </DialogContent>
    </Dialog>
  )
}
