import { FileText, Inbox, LogOut, Plug, Plus, Search, UserRound, Wrench } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router'
import { JobInbox } from '@/features/findbugs/job-inbox'
import { cn } from '@/lib/utils'
import { useAuth } from '../auth-context'
import { openPalette } from '../palette-store'
import { ThemeToggle } from '../theme-toggle'
import { engineerNav, initials } from './nav'
import { useHealth } from './use-health'
import { AccountSheet, SystemBanners } from './shared'

const tabs = [
  { to: '/jobs', label: 'Investigasi', icon: Inbox },
  { to: '/', label: 'Baru', icon: Plus, end: true },
  { to: '/koneksi', label: 'Koneksi', icon: Plug },
  { to: '/laporan', label: 'Laporan', icon: FileText },
  { to: '/tools', label: 'Tools', icon: Wrench },
]

// The list, the composer and a job are all one place: the investigations.
const investigating = (pathname: string) => pathname === '/' || pathname.startsWith('/jobs')

function RailLink({
  to,
  label,
  icon: Icon,
  end,
  active,
  alert,
}: {
  to: string
  label: string
  icon: typeof Inbox
  end?: boolean
  active?: boolean
  alert?: 'bad' | 'warn'
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        cn(
          'relative grid w-[68px] justify-items-center gap-1 rounded-lg py-2 text-[10.5px] leading-none text-rail-foreground transition-colors hover:bg-white/10 hover:text-white',
          (active ?? isActive) && 'bg-white/12 text-white',
        )
      }
    >
      <Icon className="size-[18px]" />
      {label}
      {alert && <span aria-label="Ada masalah koneksi" className={cn('absolute top-1.5 right-3 size-2 rounded-full ring-2 ring-rail', alert === 'bad' ? 'bg-bad' : 'bg-warn')} />}
    </NavLink>
  )
}

/**
 * Labelled rail; on the investigation pages the job list sits beside the
 * detail pane (Linear inbox style), and on phones list → detail with a
 * bottom tab bar. Other pages get the full width.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const { pathname } = useLocation()
  const [account, setAccount] = useState(false)
  const health = useHealth()
  if (!user) return null
  const engineer = user.role === 'engineer'
  const connAlert = health.vpnOk === false || health.splunkOk === false ? 'bad' : health.splunkBusy ? 'warn' : undefined
  const withList = investigating(pathname)
  // Phones show one pane at a time: the list on /jobs, the detail elsewhere.
  const listOnly = pathname === '/jobs'

  return (
    <div className="flex h-svh flex-col">
      <SystemBanners />
      <div className="flex min-h-0 flex-1">
        <nav aria-label="Navigasi" className="hidden w-[80px] shrink-0 flex-col items-center gap-1 bg-rail py-3 md:flex">
          <span className="mb-3 grid size-8 place-items-center rounded-lg bg-primary font-mono text-[11px] font-semibold text-primary-foreground">FB</span>
          <RailLink to="/jobs" label="Investigasi" icon={Inbox} active={investigating(pathname)} />
          <RailLink to="/koneksi" label="Koneksi" icon={Plug} alert={connAlert} />
          <RailLink to="/laporan" label="Laporan" icon={FileText} />
          <RailLink to="/tools" label="Tools" icon={Wrench} />
          {engineer && (
            <>
              <span className="my-2 h-px w-8 bg-white/10" />
              {engineerNav.map((n) => (
                <RailLink key={n.to} to={n.to} label={n.short} icon={n.icon} />
              ))}
            </>
          )}
          <div className="mt-auto grid justify-items-center gap-2">
            <button
              type="button"
              onClick={openPalette}
              title="Cari (Ctrl K)"
              className="grid w-[68px] justify-items-center gap-1 rounded-lg py-2 text-[10.5px] leading-none text-rail-foreground hover:bg-white/10 hover:text-white"
            >
              <Search className="size-[18px]" />
              Cari
            </button>
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
            <span className="relative">
              <t.icon className="size-5" />
              {t.to === '/koneksi' && connAlert && <span className={cn('absolute -top-0.5 -right-0.5 size-2 rounded-full', connAlert === 'bad' ? 'bg-bad' : 'bg-warn')} />}
            </span>
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
