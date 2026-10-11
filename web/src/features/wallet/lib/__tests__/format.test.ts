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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import zh from '@/i18n/locales/zh.json'

import { getDiscountParts } from '../format'

describe('getDiscountParts', () => {
  test('gives both the percent taken off and the Chinese 折 figure', () => {
    assert.deepEqual(getDiscountParts(0.98), { percent: 2, fold: '9.8' })
    assert.deepEqual(getDiscountParts(0.97), { percent: 3, fold: '9.7' })
    assert.deepEqual(getDiscountParts(0.95), { percent: 5, fold: '9.5' })
    assert.deepEqual(getDiscountParts(0.92), { percent: 8, fold: '9.2' })
  })

  test('drops trailing zeros and keeps two decimals at most', () => {
    assert.equal(getDiscountParts(0.9)?.fold, '9')
    assert.equal(getDiscountParts(0.5)?.fold, '5')
    assert.equal(getDiscountParts(0.975)?.fold, '9.75')
    assert.equal(getDiscountParts(0.8888)?.fold, '8.89')
  })

  test('does not round a discount into a different one', () => {
    // 2.5% off must not read as 3% off
    assert.deepEqual(getDiscountParts(0.975), { percent: 2.5, fold: '9.75' })
    assert.deepEqual(getDiscountParts(0.999), { percent: 0.1, fold: '9.99' })
    assert.deepEqual(getDiscountParts(0.8888), { percent: 11.12, fold: '8.89' })
    assert.deepEqual(getDiscountParts(0.005), { percent: 99.5, fold: '0.05' })
  })

  test('shows no badge rather than one that reads as free or as no discount', () => {
    // 0.9999 would round to "10 折"; 0.0001 to "0 折" / "100% OFF"
    for (const rate of [0.9999, 0.99996, 0.0001, 0.00004]) {
      assert.equal(getDiscountParts(rate), null, String(rate))
    }
  })

  test('is null when the rate is not a real discount', () => {
    for (const rate of [
      1,
      1.2,
      0,
      -0.5,
      Number.NaN,
      Number.POSITIVE_INFINITY,
    ]) {
      assert.equal(getDiscountParts(rate), null)
    }
  })
})

describe('preset discount wording in every locale', () => {
  const locales: Record<string, Record<string, string>> = {
    en: en.translation,
    zh: zh.translation,
    'zh-TW': zhTW.translation,
    ja: ja.translation,
    fr: fr.translation,
    ru: ru.translation,
    vi: vi.translation,
  }

  test('all seven locales carry the three keys with their placeholders', () => {
    for (const [name, translation] of Object.entries(locales)) {
      const label = translation['{{percent}}% OFF']
      assert.ok(label, `${name}: discount label missing`)
      assert.match(label, /\{\{(percent|fold)\}\}/, `${name}: discount label`)
      for (const key of ['Pay {{amount}}', 'Save {{amount}}']) {
        const value = translation[key]
        assert.ok(value, `${name}: ${key} missing`)
        assert.match(value, /\{\{amount\}\}/, `${name}: ${key}`)
      }
    }
  })

  test('Chinese uses 折 with the fold figure, not a percentage', () => {
    assert.equal(zh.translation['{{percent}}% OFF'], '{{fold}} 折')
    assert.equal(zh.translation['Pay {{amount}}'], '实付 {{amount}}')
    assert.equal(zh.translation['Save {{amount}}'], '省 {{amount}}')
    assert.equal(zhTW.translation['{{percent}}% OFF'], '{{fold}} 折')
  })
})
