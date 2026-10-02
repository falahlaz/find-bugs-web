import { Plus, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { StatusDot } from '@/app/shell/shared'
import { useHealth } from '@/app/shell/use-health'
import { formatShort } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PAGE_SIZE, useJobs, useTimezone, type JobFilters } from './queries'
import { jobTone, type JobStatus } from './status'
import { StatusBadge } from './status-badge'

const stripe = { neutral: 'bg-neu', info: 'bg-info', success: 'bg-ok', warning: 'bg-warn', danger: 'bg-bad', outline: 'bg-border' } as const

const chips: { label: string; status?: string }[] = [
  { label: 'Semua' },
  { label: 'Selesai', status: 'DONE' },
  { label: 'Gagal', status: 'FAILED' },
  { label: 'Tanpa log', status: 'NO_LOGS' },
  { label: 'Analisis', status: 'ANALYZING' },
]

/** B · Triage: the always-visible job inbox next to the detail pane. */
export function TriageJobList() {
  const { user } = useAuth()
  const tz = useTimezone()
  const health = useHealth()
  const engineer = user?.role === 'engineer'
  const [text, setText] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<string | undefined>()
  const [mine, setMine] = useState(!engineer)
  const [cursors, setCursors] = useState<number[]>([])

  useEffect(() => {
    const t = setTimeout(() => {
      setSearch(text.trim())
      setCursors([])
    }, 300)
    return () => clearTimeout(t)
  }, [text])

  const filters: JobFilters = { transactionId: search || undefined, status, mine: mine ? '1' : undefined }
  const jobs = useJobs(filters, cursors.at(-1))
  const list = jobs.data ?? []

  return (
    <>
      <div className="flex flex-wrap gap-1.5 border-b px-2.5 py-2">
        <Link to="/koneksi#vpn" className="flex items-center gap-1.5 rounded-md border bg-secondary px-2 py-1 text-[11.5px] text-muted-foreground hover:text-foreground">
          <StatusDot ok={health.vpnOk} />
          VPN <b className="font-medium text-foreground">{health.vpnOk ? 'OK' : health.vpnLabel}</b>
        </Link>
        <Link to="/koneksi#splunk" className="flex items-center gap-1.5 rounded-md border bg-secondary px-2 py-1 text-[11.5px] text-muted-foreground hover:text-foreground">
          <StatusDot ok={health.splunkOk} busy={health.splunkBusy} />
          Splunk <b className="font-medium text-foreground">{health.splunkOk ? 'OK' : health.splunkLabel}</b>
        </Link>
        <span className="flex items-center gap-1.5 rounded-md border bg-secondary px-2 py-1 text-[11.5px] text-muted-foreground">
          <span className="size-2 rounded-full bg-info" aria-hidden />
          Antrean{' '}
          <b className="font-medium text-foreground tabular-nums">
            {health.queue.active}/{health.queue.max}
          </b>
        </span>
      </div>

      <div className="grid gap-2.5 border-b px-3 pt-3 pb-2.5">
        <div className="flex items-center gap-2">
          <h2 className="text-[15px] font-semibold">Investigasi</h2>
          <span className="rounded bg-secondary px-1.5 font-mono text-[11px] text-muted-foreground tabular-nums">
            {list.length}
            {list.length === PAGE_SIZE ? '+' : ''}
          </span>
          {engineer && (
            <button
              type="button"
              onClick={() => {
                setMine((m) => !m)
                setCursors([])
              }}
              className="text-xs text-muted-foreground underline-offset-2 hover:underline"
            >
              {mine ? 'Milik saya' : 'Semua user'}
            </button>
          )}
          <Link to="/" className="ml-auto inline-flex h-7 items-center gap-1 rounded-md bg-primary px-2.5 text-xs font-medium text-primary-foreground hover:brightness-110">
            <Plus className="size-3.5" />
            Baru
          </Link>
        </div>
        <label className="flex items-center gap-2 rounded-md border bg-card px-2.5 text-muted-foreground focus-within:border-ring">
          <Search className="size-4" />
          <span className="sr-only">Cari transaction ID</span>
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Cari transaction ID"
            className="min-w-0 flex-1 bg-transparent py-1.5 font-mono text-[12.5px] text-foreground outline-none placeholder:font-sans"
          />
        </label>
        <div className="flex gap-1.5 overflow-x-auto [scrollbar-width:none]">
          {chips.map((c) => (
            <button
              key={c.label}
              type="button"
              aria-pressed={status === c.status}
              onClick={() => {
                setStatus(c.status)
                setCursors([])
              }}
              className={cn(
                'shrink-0 rounded-full border px-2.5 py-1 text-xs text-muted-foreground transition-colors',
                status === c.status && 'border-foreground bg-foreground text-background',
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {jobs.isPending &&
          Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="grid gap-2 border-b px-4 py-3">
              <div className="skeleton h-3 w-2/3" />
              <div className="skeleton h-3 w-1/3" />
            </div>
          ))}
        {jobs.error && <p className="p-4 text-sm text-bad">{jobs.error.message}</p>}
        {!jobs.isPending && list.length === 0 && <p className="p-4 text-sm text-muted-foreground">Belum ada investigasi yang cocok.</p>}
        {list.map((j) => {
          const s = j.status as JobStatus
          return (
            <NavLink
              key={j.id}
              to={`/jobs/${j.id}`}
              className={({ isActive }) =>
                cn('grid grid-cols-[3px_minmax(0,1fr)] border-b transition-colors hover:bg-secondary', isActive && 'bg-selected hover:bg-selected')
              }
            >
              <span className={stripe[jobTone(s)]} aria-hidden />
              <span className="grid gap-1.5 px-3 py-2.5">
                <span className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] font-medium">{j.transactionId}</span>
                  <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">{formatShort(j.queuedAt, tz)}</span>
                </span>
                <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-muted-foreground">
                  <StatusBadge status={s} />
                  <span>{j.environment}</span>
                  <span>·</span>
                  <span>{j.timeRange}</span>
                  {engineer && !mine && (
                    <>
                      <span>·</span>
                      <span className="truncate">{j.username}</span>
                    </>
                  )}
                </span>
              </span>
            </NavLink>
          )
        })}
        {(cursors.length > 0 || list.length === PAGE_SIZE) && (
          <div className="flex justify-between gap-2 p-3 text-xs">
            {cursors.length > 0 ? (
              <button type="button" className="rounded-md border px-2.5 py-1.5 hover:bg-secondary" onClick={() => setCursors((c) => c.slice(0, -1))}>
                ← Lebih baru
              </button>
            ) : (
              <span />
            )}
            {list.length === PAGE_SIZE && (
              <button type="button" className="rounded-md border px-2.5 py-1.5 hover:bg-secondary" onClick={() => setCursors((c) => [...c, list[list.length - 1].id])}>
                Lebih lama →
              </button>
            )}
          </div>
        )}
      </div>
    </>
  )
}
