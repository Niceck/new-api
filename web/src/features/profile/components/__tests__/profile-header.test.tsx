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

import type { UserProfile } from '../../types'
import { ProfileHeader } from '../profile-header'

const dom = new Window()
const i18n = createInstance()

before(async () => {
  await i18n
    .use(initReactI18next)
    .init({ lng: 'en', resources: { en: { translation: {} } } })
})

after(() => {
  dom.close()
})

const ACCOUNT_GROUP = 'claude kiro'

function makeProfile(overrides: Partial<UserProfile> = {}): UserProfile {
  return {
    id: 42,
    username: 'alice',
    display_name: 'Alice',
    role: 1,
    email: 'alice@example.com',
    group: ACCOUNT_GROUP,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    status: 1,
    aff_count: 0,
    aff_quota: 0,
    aff_history_quota: 0,
    created_time: 0,
    ...overrides,
  }
}

function renderedText(profile: UserProfile | null, loading = false): string {
  const host = dom.document.createElement('div')
  host.innerHTML = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <ProfileHeader profile={profile} loading={loading} />
    </I18nextProvider>
  )
  return host.textContent ?? ''
}

describe('profile header account line', () => {
  test('does not show the account group to the account holder', () => {
    const text = renderedText(makeProfile())

    assert.equal(text.includes(ACCOUNT_GROUP), false)
  })

  test('shows the username and the email of the account', () => {
    const text = renderedText(makeProfile())

    assert.equal(text.includes('@alice'), true)
    assert.equal(text.includes('alice@example.com'), true)
  })

  test('separates the username and the email with a single divider', () => {
    const text = renderedText(makeProfile())

    assert.equal(text.split('•').length - 1, 1)
  })

  test('renders no divider when the account has no email', () => {
    const text = renderedText(makeProfile({ email: undefined }))

    assert.equal(text.includes('@alice'), true)
    assert.equal(text.includes('•'), false)
  })

  test('shows no account details while the profile is loading', () => {
    const text = renderedText(makeProfile(), true)

    assert.equal(text.includes('@alice'), false)
    assert.equal(text.includes(ACCOUNT_GROUP), false)
  })
})
