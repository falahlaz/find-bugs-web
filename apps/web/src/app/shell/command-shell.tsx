import { LogOut, Search, UserRound } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router'
import { cn } from '@/lib/utils'
import { useAuth } from '../auth-context'
import { openPalette } from '../palette-store'
import { ThemeToggle } from '../theme-toggle'
import { engineerNav, initials, primaryNav } from './nav'
import { AccountSheet, DesignSwitcher, StatusDot, SystemBanners } from './shared'
import { useHealth } from './use-health'

const titles: [RegExp, string][] = [
  [/^\/$/, 'Investigasi baru'],
  [/^\/jobs\/\d+/, 'Detail job'],
  [/^\/jobs/, 'Histori'],
  [/^\/koneksi/, 'Koneksi'],
  [/^\/users/, 'User'],
  [/^\/audit/, 'Audit log'],
]

function SideLink({ to, label, icon: Icon, end }: { to: string; label: string; icon: typeof Search; end?: boolean }) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground',
          isActive && 'bg-secondary font-medium text-foreground',
        )
      }
    >
      <Icon className="size-4" />
      {label}
    </NavLink>
  )
}

/**
 * A · Command (Linear/Vercel): compact sidebar with system status and ⌘K,
 * one focused column, and a floating dock on phones.
 */
export function CommandShell({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const health = useHealth()
  const { pathname } = useLocation()
  const [account, setAccount] = useState(false)
  if (!user) return null
  const title = titles.find(([re]) => re.test(pathname))?.[1] ?? 'Find Bugs'
  const engineer = user.role === 'engineer'

  return (
    <div className="min-h-svh md:grid md:grid-cols-[232px_minmax(0,1fr)]">
      <aside className="sticky top-0 hidden h-svh flex-col gap-4 border-r bg-sidebar px-2.5 py-3.5 md:flex">
        <div className="flex items-center gap-2.5 px-2 font-semibold">
          <span className="grid size-6 place-items-center rounded-md bg-foreground font-mono text-[10px] text-background">FB</span>
          Find Bugs
        </div>
        <button
          type="button"
          onClick={openPalette}
          className="flex items-center gap-2 rounded-lg border bg-card px-2.5 py-1.5 text-left text-[13px] text-muted-foreground transition-colors hover:text-foreground"
        >
          <Search className="size-4" />
          <span className="flex-1">Cari atau perintah</span>
          <kbd className="kbd">Ctrl K</kbd>
        </button>
        <nav className="grid gap-0.5">
          {primaryNav.map((n) => (
            <SideLink key={n.to} {...n} />
          ))}
          {engineer && (
            <>
              <span className="px-2 pt-3 pb-1 text-[11px] text-muted-foreground">Engineer</span>
              {engineerNav.map((n) => (
                <SideLink key={n.to} {...n} />
              ))}
            </>
          )}
        </nav>
        <div className="mt-auto grid gap-2.5 border-t px-1.5 pt-3 text-xs text-muted-foreground">
          <NavLink to="/koneksi#vpn" className="flex items-center gap-2 hover:text-foreground">
            <StatusDot ok={health.vpnOk} />
            VPN<span className="ml-auto text-foreground">{health.vpnLabel}</span>
          </NavLink>
          <NavLink to="/koneksi#splunk" className="flex items-center gap-2 hover:text-foreground">
            <StatusDot ok={health.splunkOk} busy={health.splunkBusy} />
            Splunk<span className="ml-auto text-foreground">{health.splunkLabel}</span>
          </NavLink>
          <NavLink to="/jobs" className="flex items-center gap-2 hover:text-foreground">
            <span className="inline-block size-2 rounded-full bg-info ring-3 ring-info-bg" aria-hidden />
            Antrean
            <span className="ml-auto text-foreground tabular-nums">
              {health.queue.active}/{health.queue.max}
            </span>
          </NavLink>
        </div>
        <div className="flex items-center justify-between gap-2 px-1">
          <DesignSwitcher />
          <ThemeToggle className="px-2 [&>span]:hidden" />
        </div>
        <div className="flex items-center gap-2.5 px-1.5">
          <span className="grid size-7 place-items-center rounded-full bg-gradient-to-br from-slate-400 to-slate-600 text-[11px] font-semibold text-white">
            {initials(user.username)}
          </span>
          <span className="grid min-w-0 flex-1 text-[13px] leading-tight">
            <span className="truncate">{user.username}</span>
            <span className="text-[11px] text-muted-foreground">{engineer ? 'Engineer' : 'QA'}</span>
          </span>
          <button type="button" onClick={() => void logout()} title="Keluar" aria-label="Keluar" className="rounded-md p-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground">
            <LogOut className="size-4" />
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-col">
        <header className="sticky top-0 z-20 flex h-12 items-center gap-2 border-b bg-background/85 px-4 text-[13px] text-muted-foreground backdrop-blur-md md:px-6">
          <span className="grid size-6 place-items-center rounded-md bg-foreground font-mono text-[10px] text-background md:hidden">FB</span>
          <span className="max-md:hidden">Find Bugs</span>
          <span className="max-md:hidden">/</span>
          <span className="truncate font-medium text-foreground">{title}</span>
          <span className="ml-auto flex items-center gap-3 text-xs">
            <span className="flex items-center gap-1.5">
              <StatusDot ok={health.vpnOk} />
              <span className="max-sm:hidden">VPN</span>
            </span>
            <span className="flex items-center gap-1.5">
              <StatusDot ok={health.splunkOk} busy={health.splunkBusy} />
              <span className="max-sm:hidden">Splunk</span>
            </span>
          </span>
        </header>
        <SystemBanners />
        <main className="mx-auto w-full max-w-4xl px-4 pt-6 pb-32 md:px-8 md:pt-8 md:pb-16">{children}</main>
      </div>

      <nav
        aria-label="Navigasi"
        className="bg-glass fixed bottom-[calc(14px+env(safe-area-inset-bottom))] left-1/2 z-30 flex -translate-x-1/2 gap-0.5 rounded-full border p-1.5 shadow-[0_16px_40px_-14px_rgb(0_0_0/0.45)] backdrop-blur-xl md:hidden"
      >
        {primaryNav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.end}
            className={({ isActive }) =>
              cn('grid w-14 place-items-center gap-0.5 rounded-full py-1.5 text-[10.5px] text-muted-foreground', isActive && 'bg-secondary text-foreground')
            }
          >
            <n.icon className="size-[18px]" />
            {n.short}
          </NavLink>
        ))}
        <button type="button" onClick={openPalette} className="grid w-14 place-items-center gap-0.5 rounded-full bg-primary py-1.5 text-[10.5px] text-primary-foreground">
          <Search className="size-[18px]" />
          Cari
        </button>
        <button type="button" onClick={() => setAccount(true)} className="grid w-12 place-items-center gap-0.5 rounded-full py-1.5 text-[10.5px] text-muted-foreground">
          <UserRound className="size-[18px]" />
          Akun
        </button>
      </nav>
      <AccountSheet open={account} onClose={() => setAccount(false)} />
    </div>
  )
}
