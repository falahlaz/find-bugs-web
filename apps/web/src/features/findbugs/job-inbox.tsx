import { Plus, Search, SlidersHorizontal, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { formatShort } from '@/lib/format'
import { cn } from '@/lib/utils'
import { setListFilters, useListFilters } from './list-filters'
import { PAGE_SIZE, useEnvironments, useJobs, useTimezone, type JobFilters } from './queries'
import { jobTone, type JobStatus } from './status'
import { StatusBadge } from './status-badge'

const stripe = { neutral: 'bg-neu', info: 'bg-info', success: 'bg-ok', warning: 'bg-warn', danger: 'bg-bad', outline: 'bg-border' } as const

const statuses: { label: string; status?: string }[] = [
  { label: 'Semua status' },
  { label: 'Selesai', status: 'DONE' },
  { label: 'Gagal', status: 'FAILED' },
  { label: 'Log tidak ditemukan', status: 'NO_LOGS' },
  { label: 'Analisis AI', status: 'ANALYZING' },
  { label: 'Antre', status: 'QUEUED' },
  { label: 'Kedaluwarsa', status: 'EXPIRED' },
  { label: 'Dibatalkan', status: 'CANCELLED' },
]

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

/** The always-visible job inbox next to the detail pane. */
export function JobInbox() {
  const { user } = useAuth()
  const tz = useTimezone()
  const envs = useEnvironments()
  const engineer = user?.role === 'engineer'
  const f = useListFilters()
  const mine = f.mine ?? !engineer
  const [moreOpen, setMoreOpen] = useState(false)
  const [cursors, setCursors] = useState<number[]>([])
  const search = useDebounced(f.transactionId?.trim() ?? '', 300)

  const update = (patch: Parameters<typeof setListFilters>[0]) => {
    setListFilters(patch)
    setCursors([])
  }

  const filters: JobFilters = {
    transactionId: search || undefined,
    status: f.status,
    environment: f.environment,
    from: f.from,
    to: f.to,
    mine: mine ? '1' : undefined,
  }
  const jobs = useJobs(filters, cursors.at(-1))
  const list = jobs.data ?? []

  const statusLabel = statuses.find((o) => o.status === f.status)?.label
  const active: { label: string; clear: Parameters<typeof update>[0] }[] = [
    ...(f.status ? [{ label: statusLabel ?? f.status, clear: { status: undefined } }] : []),
    ...(f.environment ? [{ label: f.environment, clear: { environment: undefined } }] : []),
    ...(f.from || f.to ? [{ label: `${f.from ?? '…'} – ${f.to ?? '…'}`, clear: { from: undefined, to: undefined } }] : []),
    ...(engineer && mine ? [{ label: 'Milik saya', clear: { mine: false } }] : []),
  ]

  return (
    <>
      <div className="grid gap-2.5 border-b px-3 pt-3 pb-2.5">
        <div className="flex items-center gap-2">
          <h2 className="text-[15px] font-semibold">Investigasi</h2>
          <span className="text-xs text-muted-foreground tabular-nums">
            {list.length}
            {list.length === PAGE_SIZE ? '+' : ''}
          </span>
          <Link to="/" className="ml-auto inline-flex h-7 items-center gap-1 rounded-md bg-primary px-2.5 text-xs font-medium text-primary-foreground hover:brightness-110">
            <Plus className="size-3.5" />
            Baru
          </Link>
        </div>
        <div className="flex gap-1.5">
          <label className="flex min-w-0 flex-1 items-center gap-2 rounded-md border bg-card px-2.5 text-muted-foreground focus-within:border-ring">
            <Search className="size-4 shrink-0" />
            <span className="sr-only">Cari transaction ID</span>
            <input
              value={f.transactionId ?? ''}
              onChange={(e) => update({ transactionId: e.target.value || undefined })}
              placeholder="Cari transaction ID"
              className="min-w-0 flex-1 bg-transparent py-1.5 font-mono text-[12.5px] text-foreground outline-none placeholder:font-sans"
            />
          </label>
          <button
            type="button"
            aria-expanded={moreOpen}
            onClick={() => setMoreOpen((o) => !o)}
            title="Filter"
            className={cn('relative grid size-[34px] shrink-0 place-items-center rounded-md border bg-card text-muted-foreground hover:text-foreground', moreOpen && 'text-foreground')}
          >
            <SlidersHorizontal className="size-4" />
            <span className="sr-only">Filter</span>
            {active.length > 0 && <span className="absolute -top-1 -right-1 grid size-4 place-items-center rounded-full bg-primary text-[10px] text-primary-foreground">{active.length}</span>}
          </button>
        </div>
        {moreOpen && (
          <div className="grid animate-rise gap-2 rounded-md border bg-muted p-2.5">
            <div className="grid grid-cols-2 gap-2">
              <label className="grid gap-1 text-xs text-muted-foreground">
                Status
                <Select className="h-8" value={f.status ?? ''} onChange={(e) => update({ status: e.target.value || undefined })}>
                  {statuses.map((o) => (
                    <option key={o.label} value={o.status ?? ''}>
                      {o.label}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="grid gap-1 text-xs text-muted-foreground">
                Environment
                <Select className="h-8" value={f.environment ?? ''} onChange={(e) => update({ environment: e.target.value || undefined })}>
                  <option value="">Semua</option>
                  {(envs.data?.environments ?? []).map((e) => (
                    <option key={e}>{e}</option>
                  ))}
                </Select>
              </label>
              <label className="grid gap-1 text-xs text-muted-foreground">
                Dari
                <Input type="date" className="h-8 text-xs" value={f.from ?? ''} onChange={(e) => update({ from: e.target.value || undefined })} />
              </label>
              <label className="grid gap-1 text-xs text-muted-foreground">
                Sampai
                <Input type="date" className="h-8 text-xs" value={f.to ?? ''} onChange={(e) => update({ to: e.target.value || undefined })} />
              </label>
            </div>
            {engineer && (
              <label className="flex items-center gap-2 text-xs">
                <input type="checkbox" checked={mine} onChange={(e) => update({ mine: e.target.checked })} className="accent-primary" />
                Hanya investigasi saya
              </label>
            )}
          </div>
        )}
        {active.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {active.map((a) => (
              <button
                key={a.label}
                type="button"
                onClick={() => update(a.clear)}
                title="Hapus filter ini"
                className="inline-flex items-center gap-1 rounded-full border bg-card px-2 py-0.5 text-xs hover:bg-secondary"
              >
                {a.label}
                <X className="size-3 text-muted-foreground" />
              </button>
            ))}
          </div>
        )}
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
        {!jobs.isPending && list.length === 0 && (
          <p className="p-4 text-sm text-muted-foreground">{active.length > 0 || search ? 'Tidak ada investigasi yang cocok dengan filter ini.' : 'Belum ada investigasi. Mulai dari tombol Baru.'}</p>
        )}
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
