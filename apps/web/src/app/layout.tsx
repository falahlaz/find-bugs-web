import { Link, NavLink, Navigate, Outlet, useLocation } from 'react-router'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useAuth } from './auth-context'
import { useSystemStatus } from './use-system-status'

const nav = [
  { to: '/', label: 'Submit', end: true },
  { to: '/jobs', label: 'Histori' },
  { to: '/vpn', label: 'VPN' },
  { to: '/splunk', label: 'Splunk' },
]

function Banners() {
  const { data } = useSystemStatus()
  if (!data?.banners.length) return null
  return (
    <div className="flex flex-col">
      {data.banners.map((b) => (
        <div
          key={b.message}
          className={cn(
            'px-6 py-2 text-sm',
            b.level === 'error' && 'bg-destructive/10 text-destructive',
            b.level === 'warning' && 'bg-amber-100 text-amber-900',
            b.level === 'info' && 'bg-secondary text-secondary-foreground',
          )}
        >
          {b.message}{' '}
          {b.action && (
            <Link to={`/${b.action}`} className="font-medium underline">
              Buka panel {b.action === 'vpn' ? 'VPN' : 'Splunk'}
            </Link>
          )}
        </div>
      ))}
    </div>
  )
}

export function Layout() {
  const { user, loading, logout } = useAuth()
  const location = useLocation()
  if (loading) return <div className="p-6 text-muted-foreground">Memuat…</div>
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />

  const items = user.role === 'engineer' ? [...nav, { to: '/users', label: 'User' }] : nav
  return (
    <div className="min-h-svh">
      <header className="flex items-center gap-6 border-b px-6 py-3">
        <span className="font-semibold">Find Bugs</span>
        <nav className="flex gap-1">
          {items.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                cn('rounded-md px-3 py-1.5 text-sm', isActive ? 'bg-secondary font-medium' : 'text-muted-foreground hover:text-foreground')
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-3 text-sm">
          <span className="text-muted-foreground">
            {user.username} · {user.role === 'engineer' ? 'Engineer' : 'QA'}
          </span>
          <Button variant="outline" size="sm" onClick={() => void logout()}>
            Keluar
          </Button>
        </div>
      </header>
      <Banners />
      <main className="mx-auto max-w-5xl p-6">
        <Outlet />
      </main>
    </div>
  )
}
