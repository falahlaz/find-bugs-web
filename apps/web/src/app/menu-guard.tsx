import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { useAuth } from './auth-context'
import { hasMenu, menus, type Menu } from './menus'

/** Renders children only when the user has menu; the home page sends them to their first menu instead. */
export function MenuGuard({ menu, children }: { menu: Menu; children: ReactNode }) {
  const { user } = useAuth()
  const { pathname } = useLocation()
  if (hasMenu(user, menu)) return <>{children}</>
  const first = menus.find((m) => hasMenu(user, m.key))
  if (pathname === '/' && first) return <Navigate to={first.to} replace />
  return (
    <Alert tone="warning">
      {first ? 'Menu ini tidak dibuka untuk akun kamu.' : 'Belum ada menu yang dibuka untuk akun kamu.'} Minta Engineer untuk mengaktifkannya di halaman User.
    </Alert>
  )
}
