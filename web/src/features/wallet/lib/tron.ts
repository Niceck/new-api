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
export function formatTronUSDTAmount(micros: number): string {
  if (!Number.isSafeInteger(micros) || micros < 0) {
    return '0.000000'
  }
  const whole = Math.floor(micros / 1_000_000)
  const fraction = String(micros % 1_000_000).padStart(6, '0')
  return `${whole}.${fraction}`
}

const tronTxIDPattern = /^[0-9a-f]{64}$/i

export function isValidTronTxID(value: string): boolean {
  return tronTxIDPattern.test(value.trim())
}

export function formatTronRemaining(milliseconds: number): string {
  const totalSeconds = Math.max(0, Math.ceil(milliseconds / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export function formatTronCNYValue(
  usdtMicros: number,
  rateCNYMicros: number
): string {
  if (
    !Number.isSafeInteger(usdtMicros) ||
    !Number.isSafeInteger(rateCNYMicros) ||
    usdtMicros < 0 ||
    rateCNYMicros < 0
  ) {
    return '0.00'
  }
  const scaledCents =
    (BigInt(usdtMicros) * BigInt(rateCNYMicros) + 5_000_000_000n) /
    10_000_000_000n
  const whole = scaledCents / 100n
  const fraction = String(scaledCents % 100n).padStart(2, '0')
  return `${whole}.${fraction}`
}
