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

import { AccountIdentity } from '@/components/account-identity'
import type { AuthUser } from '@/stores/auth-store'

const dom = new Window()
const i18n = createInstance()

before(async () => {
  await i18n.use(initReactI18next).init({
    lng: 'en',
    resources: { en: { translation: { User: 'User' } } },
  })
})

after(() => {
  dom.close()
})

const ACCOUNT_GROUP = 'claude kiro'

function makeUser(overrides: Partial<AuthUser> = {}): AuthUser {
  return {
    id: 42,
    username: 'alice',
    display_name: 'Alice',
    role: 1,
    group: ACCOUNT_GROUP,
    ...overrides,
  }
}

function renderIdentity(user: AuthUser | null) {
  const host = dom.document.createElement('div')
  host.innerHTML = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <AccountIdentity user={user} />
    </I18nextProvider>
  )
  return host
}

describe('account identity in the account menus', () => {
  test('does not show the account group to the account holder', () => {
    const text = renderIdentity(makeUser()).textContent ?? ''

    assert.equal(text.includes(ACCOUNT_GROUP), false)
  })

  test('shows the display name of the account', () => {
    const name = renderIdentity(makeUser()).querySelector(
      '[data-slot="account-name"]'
    )

    assert.equal(name?.textContent, 'Alice')
  })

  test('falls back to the username when the account has no display name', () => {
    const name = renderIdentity(
      makeUser({ display_name: undefined })
    ).querySelector('[data-slot="account-name"]')

    assert.equal(name?.textContent, 'alice')
  })

  test('renders no separator after the role', () => {
    const text = renderIdentity(makeUser()).textContent ?? ''

    assert.equal(text.includes('·'), false)
  })

  test('renders a generic name when the account has neither name', () => {
    const name = renderIdentity(
      makeUser({ display_name: '', username: '' })
    ).querySelector('[data-slot="account-name"]')

    assert.equal(name?.textContent, 'User')
  })

  test('renders a generic name when nobody is signed in', () => {
    const name = renderIdentity(null).querySelector(
      '[data-slot="account-name"]'
    )

    assert.equal(name?.textContent, 'User')
  })
})
