import { Inbox, LogOut, Plug, Plus, Search, UserRound } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router'
import { JobInbox } from '@/features/findbugs/job-inbox'
import { cn } from '@/lib/utils'
import { useAuth } from '../auth-context'
import { openPalette } from '../palette-store'
import { ThemeToggle } from '../theme-toggle'
import { engineerNav, initials } from './nav'
import { AccountSheet, SystemBanners } from './shared'

const tabs = [
  { to: '/jobs', label: 'Investigasi', icon: Inbox },
  { to: '/', label: 'Baru', icon: Plus, end: true },
  { to: '/koneksi', label: 'Koneksi', icon: Plug },
]

function RailLink({ to, label, icon: Icon, end }: { to: string; label: string; icon: typeof Inbox; end?: boolean }) {
  return (
    <NavLink
      to={to}
      end={end}
      title={label}
      aria-label={label}
      className={({ isActive }) =>
        cn('grid size-10 place-items-center rounded-lg text-rail-foreground transition-colors hover:bg-white/10 hover:text-white', isActive && 'bg-white/12 text-white')
      }
    >
      <Icon className="size-[18px]" />
    </NavLink>
  )
}

/**
 * Icon rail, the job list always beside the detail pane (Sentry issues /
 * Linear inbox style), and on phones list → detail with a bottom tab bar.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const { pathname } = useLocation()
  const [account, setAccount] = useState(false)
  if (!user) return null
  const engineer = user.role === 'engineer'
  const withList = !/^\/(users|audit)/.test(pathname)
  // Phones show one pane at a time: the list on /jobs, the detail elsewhere.
  const listOnly = pathname === '/jobs'

  return (
    <div className="flex h-svh flex-col">
      <SystemBanners />
      <div className="flex min-h-0 flex-1">
        <nav aria-label="Navigasi" className="hidden w-14 shrink-0 flex-col items-center gap-1.5 bg-rail py-3 md:flex">
          <span className="mb-2 grid size-8 place-items-center rounded-lg bg-primary font-mono text-[11px] font-semibold text-primary-foreground">FB</span>
          {tabs.map((t) => (
            <RailLink key={t.to} {...t} label={t.to === '/' ? 'Investigasi baru' : t.label} />
          ))}
          <button
            type="button"
            onClick={openPalette}
            title="Cari (Ctrl K)"
            aria-label="Cari"
            className="grid size-10 place-items-center rounded-lg text-rail-foreground hover:bg-white/10 hover:text-white"
          >
            <Search className="size-[18px]" />
          </button>
          {engineer && (
            <>
              <span className="my-1.5 h-px w-6 bg-white/10" />
              {engineerNav.map((n) => (
                <RailLink key={n.to} to={n.to} label={n.label} icon={n.icon} />
              ))}
            </>
          )}
          <div className="mt-auto grid justify-items-center gap-2">
            <ThemeToggle className="px-2 text-rail-foreground hover:bg-white/10 [&>span]:hidden" />
            <button
              type="button"
              onClick={() => void logout()}
              title={`${user.username} · Keluar`}
              aria-label="Keluar"
              className="group grid size-8 place-items-center rounded-full bg-white/10 text-[11px] font-semibold text-white"
            >
              <span className="group-hover:hidden">{initials(user.username)}</span>
              <LogOut className="hidden size-3.5 group-hover:block" />
            </button>
          </div>
        </nav>

        {withList && (
          <aside className={cn('min-h-0 w-full flex-col border-r bg-sidebar md:flex md:w-[300px] lg:w-[330px]', listOnly ? 'flex' : 'hidden')}>
            <JobInbox />
          </aside>
        )}

        <main className={cn('@container min-h-0 min-w-0 flex-1 overflow-y-auto', withList && listOnly && 'max-md:hidden')}>
          <div className="mx-auto w-full max-w-[1100px] px-4 pt-4 pb-8 md:px-7 md:pt-6">{children}</div>
        </main>
      </div>

      <nav aria-label="Navigasi" className="flex border-t bg-card px-1.5 pt-1.5 pb-[calc(6px+env(safe-area-inset-bottom))] md:hidden">
        {tabs.map((t) => (
          <NavLink
            key={t.to}
            to={t.to}
            end={t.end}
            className={({ isActive }) =>
              cn('grid flex-1 place-items-center gap-0.5 rounded-lg py-1 text-[10.5px] text-muted-foreground', (isActive || (t.to === '/jobs' && pathname.startsWith('/jobs'))) && 'text-primary')
            }
          >
            <t.icon className="size-5" />
            {t.label}
          </NavLink>
        ))}
        <button type="button" onClick={openPalette} className="grid flex-1 place-items-center gap-0.5 py-1 text-[10.5px] text-muted-foreground">
          <Search className="size-5" />
          Cari
        </button>
        <button type="button" onClick={() => setAccount(true)} className="grid flex-1 place-items-center gap-0.5 py-1 text-[10.5px] text-muted-foreground">
          <UserRound className="size-5" />
          Akun
        </button>
      </nav>
      <AccountSheet open={account} onClose={() => setAccount(false)} />
    </div>
  )
}
