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
import { useUserDisplay } from '@/hooks/use-user-display'
import type { AuthUser } from '@/stores/auth-store'

type AccountIdentityProps = {
  user: AuthUser | null | undefined
}

/**
 * Name and role of the signed-in account, shown at the top of the account
 * menus (desktop profile dropdown and mobile drawer).
 *
 * The account-level group is deliberately left out. On this site a line is
 * chosen per API key, and the account group only acts as the fallback for keys
 * without one, so showing it made every customer look assigned to that line.
 */
export function AccountIdentity({ user }: AccountIdentityProps) {
  const { displayName, roleLabel } = useUserDisplay(user)

  return (
    <div className='flex flex-1 flex-col gap-0.5 overflow-hidden'>
      <p
        data-slot='account-name'
        className='text-foreground truncate text-sm font-medium'
      >
        {displayName}
      </p>
      <span data-slot='account-role' className='text-muted-foreground text-xs'>
        {roleLabel}
      </span>
    </div>
  )
}
