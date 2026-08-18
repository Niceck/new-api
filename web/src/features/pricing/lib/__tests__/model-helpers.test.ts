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

import { getDisplayGroupForBadge } from '../model-helpers.ts'

// Mock FILTER_ALL constant used by the function
const FILTER_ALL = '全部'

describe('getDisplayGroupForBadge', () => {
  test('显示选中分组,当模型属于该分组时', () => {
    const groups = ['claude MAX', '超级稳定claude']
    const result = getDisplayGroupForBadge(groups, '超级稳定claude')
    assert.equal(result, '超级稳定claude')
  })

  test('回退到第一个分组,当选中分组不在模型分组列表中时', () => {
    const groups = ['claude MAX', '超级稳定claude']
    const result = getDisplayGroupForBadge(groups, 'Codex PRO')
    assert.equal(result, 'claude MAX')
  })

  test('回退到第一个分组,当未选中任何分组时', () => {
    const groups = ['claude MAX', '超级稳定claude']
    const result = getDisplayGroupForBadge(groups, undefined)
    assert.equal(result, 'claude MAX')
  })

  test('回退到第一个分组,当选中"全部"时', () => {
    const groups = ['claude MAX', '超级稳定claude']
    const result = getDisplayGroupForBadge(groups, FILTER_ALL)
    assert.equal(result, 'claude MAX')
  })

  test('返回undefined,当模型没有任何分组时', () => {
    const result = getDisplayGroupForBadge([], '超级稳定claude')
    assert.equal(result, undefined)
  })

  test('返回唯一分组,即使它不是选中的分组', () => {
    const groups = ['claude MAX']
    const result = getDisplayGroupForBadge(groups, '超级稳定claude')
    assert.equal(result, 'claude MAX')
  })
})
