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
import { after, before, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import { GroupBadge } from '@/components/group-badge'

// No top-level await: while it is pending Bun starts the next test file, and
// registering these cases afterwards fails as "test() inside another test()".
const dom = new Window()
const i18n = createInstance()

before(async () => {
  await i18n.use(initReactI18next).init({
    lng: 'en',
    resources: {
      en: { translation: { Auto: 'Auto', 'User Group': 'User Group' } },
    },
  })
})

const TEXT_COLOR_CLASSES = [
  'text-chart-1',
  'text-chart-2',
  'text-chart-3',
  'text-chart-4',
  'text-chart-5',
  'text-destructive',
  'text-info',
  'text-muted-foreground',
  'text-success',
  'text-warning',
]

/** Renders the badge and returns the single text color class it carries. */
function renderedTextColor(props: {
  group?: string | null
  ratio?: number
}): string {
  const host = dom.document.createElement('div')
  host.innerHTML = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <GroupBadge group={props.group} ratio={props.ratio} />
    </I18nextProvider>
  )
  const badge = host.querySelector('[data-slot="status-badge"]')
  assert.ok(badge, 'group badge should render')
  const applied = TEXT_COLOR_CLASSES.filter((name) =>
    badge.classList.contains(name)
  )
  assert.equal(applied.length, 1, `text color classes: ${applied.join(',')}`)
  return applied[0]
}

describe('group badge color', () => {
  after(() => {
    dom.close()
  })

  test('shows the main Claude line in the color it had before the rename', () => {
    assert.equal(renderedTextColor({ group: '满血稳定claude' }), 'text-chart-2')
  })

  test('shows the Codex line in the color it had before the rename', () => {
    assert.equal(renderedTextColor({ group: '满血稳定codex' }), 'text-chart-3')
  })

  test('keeps the kiro line on its name-derived red', () => {
    assert.equal(
      renderedTextColor({ group: 'claude kiro' }),
      'text-destructive'
    )
  })

  test('keeps the pinned color when the token list shows a ratio beside it', () => {
    assert.equal(
      renderedTextColor({ group: '满血稳定claude', ratio: 1.75 }),
      'text-chart-2'
    )
  })

  test('applies the pinned color to a group name padded with whitespace', () => {
    assert.equal(
      renderedTextColor({ group: ' 满血稳定codex ' }),
      'text-chart-3'
    )
  })

  test('derives the color from the name for groups that are not pinned', () => {
    assert.equal(renderedTextColor({ group: 'vip' }), 'text-chart-1')
  })

  test('keeps the auto group neutral', () => {
    assert.equal(renderedTextColor({ group: 'auto' }), 'text-muted-foreground')
  })

  test('keeps a missing group neutral', () => {
    assert.equal(renderedTextColor({ group: '' }), 'text-muted-foreground')
    assert.equal(renderedTextColor({ group: null }), 'text-muted-foreground')
  })
})
