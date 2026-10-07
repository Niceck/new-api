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

import {
  buildPriceLines,
  formatDiscount,
  getDiscountFraction,
  getOfficialQuote,
  getSharedInputLimit,
  stripGroupLabelNote,
} from '../price-table.ts'

// 线上 /api/pricing 的真实形态（2026-10-06 抓取后裁剪）。
const LIVE = {
  success: true,
  group_ratio: { 满血稳定codex: 0.4, 'claude kiro': 0.4, 满血稳定claude: 1.75 },
  usable_group: {
    满血稳定codex: '普通线路pro（0.6折）',
    'claude kiro': '普通线路claude（0.6折）',
    满血稳定claude: '满血稳定claude（2.5折）',
  },
  data: [
    {
      model_name: 'claude-opus-5-5',
      quota_type: 0,
      model_ratio: 2,
      completion_ratio: 5,
      enable_groups: ['claude kiro', '满血稳定claude'],
    },
    {
      model_name: 'claude-fable-5',
      quota_type: 0,
      model_ratio: 5,
      completion_ratio: 5,
      enable_groups: ['满血稳定claude'],
    },
    {
      model_name: 'claude-haiku-4-5-20251001',
      quota_type: 0,
      model_ratio: 0.5,
      completion_ratio: 5,
      enable_groups: ['claude kiro', '满血稳定claude'],
    },
    {
      model_name: 'gpt-6-sol',
      quota_type: 0,
      model_ratio: 1,
      completion_ratio: 5,
      enable_groups: ['满血稳定codex'],
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 272000 ? tier("standard", p * 2 + c * 10 + cr * 0.2 + cc * 2.5) : tier("long_context", p * 4 + c * 15 + cr * 0.4 + cc * 5)',
    },
  ],
}

const OPTIONS = {
  headlineModels: ['claude-opus-5-5', 'gpt-6-sol', 'gpt-6-astra'],
  lineOrder: ['满血稳定claude', 'claude kiro', '满血稳定codex'],
}

describe('stripGroupLabelNote', () => {
  test('去掉分组显示名末尾的括注（全角与半角）', () => {
    assert.equal(
      stripGroupLabelNote('满血稳定claude（2.5折）'),
      '满血稳定claude'
    )
    assert.equal(stripGroupLabelNote('普通线路pro (0.6折)'), '普通线路pro')
  })

  test('没有括注或整段都是括注时原样返回', () => {
    assert.equal(stripGroupLabelNote('满血稳定codex'), '满血稳定codex')
    assert.equal(stripGroupLabelNote('（内测）'), '（内测）')
  })
})

describe('getDiscountFraction', () => {
  test('人民币 1:1 口径下按 1 美元≈7 元粗算', () => {
    const stable = getDiscountFraction(1.75, {
      quotaDisplayType: 'CNY',
      usdExchangeRate: 1,
    })
    assert.equal(stable, 0.25)
    const normal = getDiscountFraction(0.4, {
      quotaDisplayType: 'CNY',
      usdExchangeRate: 1,
    })
    assert.ok(normal !== null && Math.abs(normal - 0.4 / 7) < 1e-12)
  })

  test('人民币按真实汇率展示时折扣就是倍率', () => {
    assert.equal(
      getDiscountFraction(0.5, { quotaDisplayType: 'CNY', usdExchangeRate: 7 }),
      0.5
    )
  })

  test('美元展示时折扣就是倍率', () => {
    assert.equal(
      getDiscountFraction(0.4, { quotaDisplayType: 'USD', usdExchangeRate: 7 }),
      0.4
    )
  })

  test('不涨价也不是有效倍率时不给折扣', () => {
    assert.equal(
      getDiscountFraction(8, { quotaDisplayType: 'CNY', usdExchangeRate: 1 }),
      null
    )
    assert.equal(
      getDiscountFraction(0, { quotaDisplayType: 'CNY', usdExchangeRate: 1 }),
      null
    )
    assert.equal(
      getDiscountFraction(Number.NaN, {
        quotaDisplayType: 'CNY',
        usdExchangeRate: 1,
      }),
      null
    )
  })

  test('按 tokens 或自定义货币展示时不给折扣', () => {
    assert.equal(
      getDiscountFraction(0.4, {
        quotaDisplayType: 'TOKENS',
        usdExchangeRate: 1,
      }),
      null
    )
    assert.equal(
      getDiscountFraction(0.4, {
        quotaDisplayType: 'CUSTOM',
        usdExchangeRate: 1,
      }),
      null
    )
  })
})

describe('formatDiscount', () => {
  test('同时给出「折」与百分比两种写法，保留一位小数并去掉多余的 0', () => {
    assert.deepEqual(formatDiscount(0.25), { zhe: '2.5', percent: '25' })
    assert.deepEqual(formatDiscount(0.4 / 7), { zhe: '0.6', percent: '5.7' })
    assert.deepEqual(formatDiscount(0.5), { zhe: '5', percent: '50' })
  })
})

const SOL = LIVE.data[3]

describe('getOfficialQuote', () => {
  test('按倍率计价：模型倍率 1 = 每百万 tokens 2 美元', () => {
    assert.deepEqual(getOfficialQuote(LIVE.data[0]), {
      kind: 'priced',
      input: 4,
      output: 20,
      inputLimit: null,
    })
  })

  test('分档计价：报标准档单价，并带出它适用的输入长度上限', () => {
    assert.deepEqual(getOfficialQuote(SOL), {
      kind: 'priced',
      input: 2,
      output: 10,
      inputLimit: 272000,
    })
  })

  test('分档系数就是美元单价，不再乘模型倍率或输出倍率', () => {
    const quote = getOfficialQuote({
      ...SOL,
      model_ratio: 99,
      completion_ratio: 99,
    })
    assert.deepEqual(quote, {
      kind: 'priced',
      input: 2,
      output: 10,
      inputLimit: 272000,
    })
  })

  test('只有一档且无条件时，价格不随输入长度变化', () => {
    assert.deepEqual(
      getOfficialQuote({
        ...SOL,
        billing_expr: 'tier("base", p * 5 + c * 25)',
      }),
      { kind: 'priced', input: 5, output: 25, inputLimit: null }
    )
    assert.deepEqual(
      getOfficialQuote({
        ...SOL,
        billing_expr: 'v1:tier("base", p * 5 + c * 25)',
      }),
      { kind: 'priced', input: 5, output: 25, inputLimit: null }
    )
  })

  test('看不懂的分档形态一律标成无法报价，绝不回退到模型倍率', () => {
    const unsupported = { kind: 'unsupported' }
    const exprs = [
      undefined,
      '',
      'p * 2 + c * 10',
      // 第一档是长上下文档（下界条件），不是普通请求的价格
      'len > 272000 ? tier("long", p * 4 + c * 15) : tier("std", p * 2 + c * 10)',
      // 档位条件不是输入长度
      'p <= 1000 ? tier("a", p * 2 + c * 10) : tier("b", p * 4 + c * 15)',
      // 多重条件
      'len <= 272000 && c <= 10 ? tier("a", p * 2 + c * 10) : tier("b", p * 4 + c * 15)',
      // 附加请求规则会按条件再乘系数
      '(tier("base", p * 5 + c * 25)) * (header("anthropic-beta") has "fast-mode" ? 6 : 1)',
      // 缺输出单价
      'tier("base", p * 5)',
      // 只有一档却带条件：超出后没有价格可言
      'len <= 272000 ? tier("only", p * 2 + c * 10) : p * 0 + c * 0',
      // 表达式里夹了编辑器写不出来的内容
      'len <= 272000 ? tier("a", p * 2 + c * 10) : tier("b", p * 4 + c * 15) + 1',
    ]
    for (const billing_expr of exprs) {
      assert.deepEqual(
        getOfficialQuote({ ...SOL, billing_expr }),
        unsupported,
        String(billing_expr)
      )
    }
  })

  test('输入上界写成 < 时，上限取能落在这一档里的最大整数', () => {
    const quote = getOfficialQuote({
      ...SOL,
      billing_expr:
        'len < 272000 ? tier("standard", p * 2 + c * 10) : tier("long_context", p * 4 + c * 15)',
    })
    assert.deepEqual(quote, {
      kind: 'priced',
      input: 2,
      output: 10,
      inputLimit: 271999,
    })
  })

  test('输入上界不是正整数时不报价', () => {
    for (const bound of ['272000.5', '0.5']) {
      assert.deepEqual(
        getOfficialQuote({
          ...SOL,
          billing_expr: `len <= ${bound} ? tier("a", p * 2 + c * 10) : tier("b", p * 4 + c * 15)`,
        }),
        { kind: 'unsupported' },
        bound
      )
    }
  })

  test('不认识的计费模式标成无法报价，不当成按倍率计价', () => {
    assert.deepEqual(
      getOfficialQuote({ ...LIVE.data[0], billing_mode: 'per_second' }),
      { kind: 'unsupported' }
    )
    assert.deepEqual(getOfficialQuote({ ...LIVE.data[0], billing_mode: 7 }), {
      kind: 'unsupported',
    })
  })

  test('计费模式为空串或缺省时按倍率计价', () => {
    for (const billing_mode of ['', undefined, null]) {
      assert.deepEqual(getOfficialQuote({ ...LIVE.data[0], billing_mode }), {
        kind: 'priced',
        input: 4,
        output: 20,
        inputLimit: null,
      })
    }
  })

  test('计费类型必须明确是按 token（数字 0），其余一律不报价', () => {
    for (const quota_type of [1, '1', '0', undefined, null, 2]) {
      assert.equal(
        getOfficialQuote({ ...LIVE.data[0], quota_type }),
        null,
        String(quota_type)
      )
    }
  })

  test('倍率非法、算出来不是有限数、不是对象时返回 null', () => {
    assert.equal(getOfficialQuote({ ...LIVE.data[0], model_ratio: 'x' }), null)
    assert.equal(
      getOfficialQuote({ ...LIVE.data[0], completion_ratio: -1 }),
      null
    )
    assert.equal(getOfficialQuote({ ...LIVE.data[0], model_ratio: 0 }), null)
    assert.equal(
      getOfficialQuote({ ...LIVE.data[0], model_ratio: 1e308 }),
      null
    )
    assert.equal(
      getOfficialQuote({
        ...LIVE.data[0],
        model_ratio: 1e200,
        completion_ratio: 1e200,
      }),
      null
    )
    assert.equal(getOfficialQuote(null), null)
  })
})

describe('buildPriceLines', () => {
  test('按给定顺序列出线路，只收标题模型，本站价 = 官网价 × 倍率', () => {
    const lines = buildPriceLines(LIVE, OPTIONS)
    assert.deepEqual(
      lines.map((line) => line.group),
      ['满血稳定claude', 'claude kiro', '满血稳定codex']
    )
    assert.equal(lines[0].label, '满血稳定claude')
    assert.equal(lines[0].ratio, 1.75)
    assert.deepEqual(lines[0].rows, [
      {
        model: 'claude-opus-5-5',
        quote: {
          officialInput: 4,
          officialOutput: 20,
          siteInput: 7,
          siteOutput: 35,
          inputLimit: null,
        },
      },
    ])
    assert.deepEqual(lines[2].rows, [
      {
        model: 'gpt-6-sol',
        quote: {
          officialInput: 2,
          officialOutput: 10,
          siteInput: 0.8,
          siteOutput: 4,
          inputLimit: 272000,
        },
      },
    ])
  })

  test('分档形态看不懂的模型仍列在线路里，但不给价格', () => {
    const data = {
      ...LIVE,
      data: [...LIVE.data.slice(0, 3), { ...SOL, billing_expr: 'p * 2' }],
    }
    const lines = buildPriceLines(data, OPTIONS)
    assert.deepEqual(lines[2].rows, [{ model: 'gpt-6-sol', quote: null }])
  })

  test('行顺序跟随标题模型清单，而不是接口返回顺序', () => {
    const lines = buildPriceLines(LIVE, {
      headlineModels: ['claude-haiku-4-5-20251001', 'claude-opus-5-5'],
      lineOrder: ['claude kiro'],
    })
    assert.deepEqual(
      lines[0].rows.map((row) => row.model),
      ['claude-haiku-4-5-20251001', 'claude-opus-5-5']
    )
  })

  test('清单外的分组一律不展示，哪怕接口把它连同倍率一起返回', () => {
    const withInternal = {
      ...LIVE,
      group_ratio: { ...LIVE.group_ratio, sole: 1.5 },
      usable_group: { ...LIVE.usable_group, sole: 'sole' },
      data: LIVE.data.map((model) => ({
        ...model,
        enable_groups: [...model.enable_groups, 'sole'],
      })),
    }
    const lines = buildPriceLines(withInternal, OPTIONS)
    assert.deepEqual(
      lines.map((line) => line.group),
      ['满血稳定claude', 'claude kiro', '满血稳定codex']
    )
  })

  test('清单里的分组没有标题模型时不出现', () => {
    const lines = buildPriceLines(LIVE, {
      headlineModels: ['claude-opus-5-5'],
      lineOrder: ['满血稳定codex', 'claude kiro'],
    })
    assert.deepEqual(
      lines.map((line) => line.group),
      ['claude kiro']
    )
  })

  test('分组没有显示名时用分组键；显示名为对象时取 desc', () => {
    const lines = buildPriceLines(
      {
        ...LIVE,
        usable_group: { 'claude kiro': { desc: '普通线路（内测）', ratio: 1 } },
      },
      OPTIONS
    )
    const byGroup = new Map(lines.map((line) => [line.group, line.label]))
    assert.equal(byGroup.get('claude kiro'), '普通线路')
    assert.equal(byGroup.get('满血稳定codex'), '满血稳定codex')
  })

  test('倍率缺失或非法的分组整条不出，不拿 1 顶替', () => {
    const lines = buildPriceLines(
      { ...LIVE, group_ratio: { 'claude kiro': 0.4, 满血稳定claude: 'bad' } },
      OPTIONS
    )
    assert.deepEqual(
      lines.map((line) => line.group),
      ['claude kiro']
    )
  })

  test('接口没有明确成功时，即使带着像样的数据也不出价格', () => {
    assert.deepEqual(buildPriceLines({ ...LIVE, success: false }, OPTIONS), [])
    assert.deepEqual(buildPriceLines({ ...LIVE, success: 'true' }, OPTIONS), [])
    const { success: _success, ...withoutSuccess } = LIVE
    assert.deepEqual(buildPriceLines(withoutSuccess, OPTIONS), [])
  })

  test('本站价算出来不是有限数时，该模型在这条线路上不给价格', () => {
    const lines = buildPriceLines(
      { ...LIVE, group_ratio: { ...LIVE.group_ratio, 满血稳定claude: 1e308 } },
      OPTIONS
    )
    assert.deepEqual(lines[0].rows, [{ model: 'claude-opus-5-5', quote: null }])
    assert.equal(lines[1].rows[0].quote?.siteInput, 1.6)
  })

  test('接口数据形态不对时返回空数组', () => {
    assert.deepEqual(buildPriceLines(null, OPTIONS), [])
    assert.deepEqual(buildPriceLines({ success: true, data: 'x' }, OPTIONS), [])
    assert.deepEqual(buildPriceLines({ success: true, data: [] }, OPTIONS), [])
    assert.deepEqual(
      buildPriceLines({ ...LIVE, data: [null, 1, { model_name: 5 }] }, OPTIONS),
      []
    )
  })

  test('不改动传入的数据', () => {
    const before = JSON.stringify(LIVE)
    buildPriceLines(LIVE, OPTIONS)
    assert.equal(JSON.stringify(LIVE), before)
  })
})

describe('getSharedInputLimit', () => {
  test('所有分档报价的输入上限一致时返回该上限', () => {
    assert.equal(getSharedInputLimit(buildPriceLines(LIVE, OPTIONS)), 272000)
  })

  test('没有分档报价时为 null', () => {
    const lines = buildPriceLines(LIVE, {
      headlineModels: ['claude-opus-5-5'],
      lineOrder: [],
    })
    assert.equal(getSharedInputLimit(lines), null)
  })

  test('上限不一致时为 null，页面改用不带数字的说明', () => {
    const data = {
      ...LIVE,
      data: [
        ...LIVE.data,
        {
          ...SOL,
          model_name: 'gpt-6-astra',
          billing_expr:
            'len <= 100000 ? tier("standard", p * 10 + c * 50) : tier("long_context", p * 20 + c * 75)',
        },
      ],
    }
    assert.equal(getSharedInputLimit(buildPriceLines(data, OPTIONS)), null)
  })
})
