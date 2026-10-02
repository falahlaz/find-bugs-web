import { Clock, LayoutGrid, LogOut, Plug, Plus, Search, UserRound } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation } from 'react-router'
import { SubmitForm } from '@/features/findbugs/submit-form'
import { cn } from '@/lib/utils'
import { useAuth } from '../auth-context'
import { openPalette } from '../palette-store'
import { ThemeToggle } from '../theme-toggle'
import { engineerNav, initials } from './nav'
import { AccountSheet, BottomSheet, DesignSwitcher, StatusDot, SystemBanners } from './shared'
import { useHealth } from './use-health'

const pills = [
  { to: '/', label: 'Beranda', icon: LayoutGrid, end: true },
  { to: '/jobs', label: 'Histori', icon: Clock },
  { to: '/koneksi', label: 'Koneksi', icon: Plug },
]

/**
 * C · Bento Live: a light top bar with pill navigation, a tile-based home,
 * and on phones a floating glass tab bar whose + opens a submit sheet.
 */
export function BentoShell({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const health = useHealth()
  const { pathname } = useLocation()
  const [sheet, setSheet] = useState(false)
  const [account, setAccount] = useState(false)
  if (!user) return null
  const engineer = user.role === 'engineer'
  const active = (to: string) => (to === '/' ? pathname === '/' : pathname.startsWith(to))

  return (
    <div className="min-h-svh">
      <header className="sticky top-0 z-20 bg-background/80 backdrop-blur-md">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-4 py-3 md:px-6">
          <Link to="/" className="flex items-center gap-2 font-display text-[19px] font-bold tracking-tight">
            <span className="relative size-6 rounded-lg bg-hero after:absolute after:inset-[6px] after:rounded-full after:border-2 after:border-hero-foreground after:border-r-transparent" />
            find bugs
          </Link>
          <nav aria-label="Navigasi" className="mx-auto hidden items-center gap-0.5 rounded-full border bg-card p-1 md:flex">
            {pills.map((p) => (
              <NavLink
                key={p.to}
                to={p.to}
                end={p.end}
                className={cn('rounded-full px-4 py-1.5 text-[13px] font-medium text-muted-foreground transition-colors', active(p.to) && 'bg-foreground text-background')}
              >
                {p.label}
              </NavLink>
            ))}
            {engineer &&
              engineerNav.map((n) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  className={({ isActive }) => cn('rounded-full px-3 py-1.5 text-[13px] text-muted-foreground', isActive && 'bg-foreground text-background')}
                >
                  {n.short}
                </NavLink>
              ))}
          </nav>
          <div className="ml-auto flex items-center gap-2 md:ml-0">
            <button type="button" onClick={openPalette} className="hidden size-9 place-items-center rounded-full border bg-card text-muted-foreground hover:text-foreground md:grid" aria-label="Cari (Ctrl K)" title="Cari (Ctrl K)">
              <Search className="size-4" />
            </button>
            <Link to="/koneksi" className="flex items-center gap-2 rounded-full border bg-card px-3 py-2 text-xs text-muted-foreground" aria-label="Status sistem">
              <StatusDot ok={health.vpnOk} />
              <StatusDot ok={health.splunkOk} busy={health.splunkBusy} />
              <span className="max-sm:hidden">{health.allOk ? 'Sistem normal' : 'Perlu tindakan'}</span>
            </Link>
            <DesignSwitcher className="max-md:hidden" />
            <ThemeToggle className="max-md:hidden [&>span]:hidden" />
            <button
              type="button"
              onClick={() => void logout()}
              title={`${user.username} · Keluar`}
              className="group grid size-9 place-items-center rounded-full bg-gradient-to-br from-orange-300 to-rose-500 text-xs font-bold text-white max-md:hidden"
            >
              <span className="group-hover:hidden">{initials(user.username)}</span>
              <LogOut className="hidden size-4 group-hover:block" />
            </button>
          </div>
        </div>
      </header>
      <SystemBanners className="mx-auto max-w-6xl overflow-hidden px-4 md:px-6 [&>div]:rounded-xl [&>div+div]:mt-2" />
      <main className="mx-auto max-w-6xl px-3 pt-2 pb-32 md:px-6 md:pt-4 md:pb-16">{children}</main>

      <nav
        aria-label="Navigasi"
        className="bg-glass fixed inset-x-3 bottom-[calc(12px+env(safe-area-inset-bottom))] z-30 flex items-center justify-around rounded-[22px] border p-1.5 shadow-[0_16px_40px_-16px_rgb(10_20_60/0.45)] backdrop-blur-xl md:hidden"
      >
        {pills.slice(0, 2).map((p) => (
          <NavLink key={p.to} to={p.to} end={p.end} className={cn('grid place-items-center gap-0.5 rounded-xl px-3 py-1.5 text-[10.5px] font-medium text-muted-foreground', active(p.to) && 'text-foreground')}>
            <p.icon className="size-5" />
            {p.label}
          </NavLink>
        ))}
        <button
          type="button"
          onClick={() => setSheet(true)}
          aria-label="Investigasi baru"
          className="-mt-7 grid size-13 place-items-center rounded-[18px] bg-hero text-hero-foreground shadow-[0_10px_24px_-8px_var(--hero)] active:scale-95"
        >
          <Plus className="size-6" />
        </button>
        <NavLink to="/koneksi" className={cn('grid place-items-center gap-0.5 rounded-xl px-3 py-1.5 text-[10.5px] font-medium text-muted-foreground', active('/koneksi') && 'text-foreground')}>
          <Plug className="size-5" />
          Koneksi
        </NavLink>
        <button type="button" onClick={() => setAccount(true)} className="grid place-items-center gap-0.5 rounded-xl px-3 py-1.5 text-[10.5px] font-medium text-muted-foreground">
          <UserRound className="size-5" />
          Akun
        </button>
      </nav>
      <BottomSheet open={sheet} onClose={() => setSheet(false)} title="Investigasi baru">
        {sheet && <SubmitForm idPrefix="sheet" onSubmitted={() => setSheet(false)} />}
      </BottomSheet>
      <AccountSheet open={account} onClose={() => setAccount(false)} />
    </div>
  )
}
