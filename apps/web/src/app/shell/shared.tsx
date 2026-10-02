import { X } from 'lucide-react'
import { useEffect, type ReactNode } from 'react'
import { Link, NavLink } from 'react-router'
import { cn } from '@/lib/utils'
import { useAuth } from '../auth-context'
import { designs, useDesign } from '../design'
import { ThemeToggle } from '../theme-toggle'
import { useSystemStatus } from '../use-system-status'
import { engineerNav } from './nav'

export function StatusDot({ ok, busy = false, className }: { ok: boolean | null; busy?: boolean; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        'inline-block size-2 shrink-0 rounded-full',
        busy ? 'animate-pulse bg-warn ring-3 ring-warn-bg' : ok == null ? 'bg-neu' : ok ? 'bg-ok ring-3 ring-ok-bg' : 'bg-bad ring-3 ring-bad-bg',
        className,
      )}
    />
  )
}

/** Global VPN/Splunk banners from the server. */
export function SystemBanners({ className }: { className?: string }) {
  const { data } = useSystemStatus()
  if (!data?.banners.length) return null
  return (
    <div className={cn('flex flex-col', className)}>
      {data.banners.map((b) => (
        <div
          key={b.message}
          className={cn(
            'flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-[13px] sm:px-6',
            b.level === 'error' && 'bg-bad-bg',
            b.level === 'warning' && 'bg-warn-bg',
            b.level === 'info' && 'bg-info-bg',
          )}
        >
          <StatusDot ok={b.level === 'info'} busy={b.level === 'warning'} />
          <span>{b.message}</span>
          {b.action && (
            <Link to={`/koneksi#${b.action}`} className="font-medium underline underline-offset-2">
              Buka Koneksi
            </Link>
          )}
        </div>
      ))}
    </div>
  )
}

/** A/B/C switch between the three explored designs. */
export function DesignSwitcher({ className, full = false, vertical = false }: { className?: string; full?: boolean; vertical?: boolean }) {
  const { design, setDesign } = useDesign()
  return (
    <div
      role="group"
      aria-label="Desain"
      className={cn('inline-flex rounded-lg border bg-muted p-0.5', full && 'flex w-full', vertical && 'flex-col', className)}
    >
      {designs.map((d) => (
        <button
          key={d.id}
          type="button"
          title={`${d.short} · ${d.label}: ${d.hint}`}
          aria-pressed={design === d.id}
          onClick={() => setDesign(d.id)}
          className={cn(
            'rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors',
            full && 'flex-1 py-1.5',
            design === d.id && 'bg-card text-foreground shadow-xs',
          )}
        >
          {full ? `${d.short} · ${d.label}` : d.short}
        </button>
      ))}
    </div>
  )
}

/** Slide-up panel for phones (account menu, quick submit). */
export function BottomSheet({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: ReactNode }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  return (
    <>
      <div
        aria-hidden
        onClick={onClose}
        className={cn('fixed inset-0 z-40 bg-black/40 transition-opacity duration-300', open ? 'opacity-100' : 'pointer-events-none opacity-0')}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        inert={!open}
        className={cn(
          'fixed inset-x-0 bottom-0 z-50 max-h-[88dvh] overflow-y-auto rounded-t-3xl border-t bg-card px-5 pt-3 pb-[calc(1.5rem+env(safe-area-inset-bottom))] shadow-[0_-20px_50px_-20px_rgb(0_0_0/0.4)] transition-[transform,visibility] duration-300 ease-[cubic-bezier(.2,.9,.3,1)]',
          open ? 'visible translate-y-0' : 'invisible translate-y-[105%]',
        )}
      >
        <div className="mx-auto mb-3 h-1.5 w-10 rounded-full bg-border" />
        <div className="mb-4 flex items-center justify-between">
          <h2 className="font-display text-lg font-semibold">{title}</h2>
          <button type="button" onClick={onClose} className="rounded-full p-1.5 text-muted-foreground hover:bg-secondary" aria-label="Tutup">
            <X className="size-5" />
          </button>
        </div>
        {children}
      </div>
    </>
  )
}

/** Account, admin links, theme and design, for the phone navigation. */
export function AccountSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { user, logout } = useAuth()
  if (!user) return null
  return (
    <BottomSheet open={open} onClose={onClose} title={user.username}>
      <div className="grid gap-4">
        <p className="-mt-3 text-sm text-muted-foreground">{user.role === 'engineer' ? 'Engineer' : 'QA'}</p>
        {user.role === 'engineer' && (
          <nav className="grid gap-1">
            {engineerNav.map((n) => (
              <NavLink key={n.to} to={n.to} onClick={onClose} className="flex items-center gap-3 rounded-lg px-3 py-2.5 hover:bg-secondary">
                <n.icon className="size-4 text-muted-foreground" />
                {n.label}
              </NavLink>
            ))}
          </nav>
        )}
        <div className="grid gap-2">
          <span className="text-xs font-medium text-muted-foreground">Desain</span>
          <DesignSwitcher full />
        </div>
        <div className="flex items-center justify-between gap-2">
          <ThemeToggle />
          <button type="button" onClick={() => void logout()} className="rounded-lg border px-3 py-2 text-sm font-medium hover:bg-secondary">
            Keluar
          </button>
        </div>
      </div>
    </BottomSheet>
  )
}

