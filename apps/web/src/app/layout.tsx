import { Navigate, Outlet, useLocation } from 'react-router'
import { useAuth } from './auth-context'
import { CommandPalette } from './command-palette'
import { AppShell } from './shell/app-shell'

export function Layout() {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <div className="p-6 text-muted-foreground">Memuat…</div>
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return (
    <>
      <AppShell>
        <Outlet />
      </AppShell>
      <CommandPalette />
    </>
  )
}
