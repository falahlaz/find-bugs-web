import { Search, SlidersHorizontal } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { useDesign } from '@/app/design'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { JobCards, JobRows } from './job-list-views'
import { PAGE_SIZE, useEnvironments, useJobs, type JobFilters } from './queries'
import { TriageOverview } from './triage-overview'

const filterKeys = ['environment', 'status', 'transactionId', 'from', 'to'] as const
const statusChips: { label: string; status?: string }[] = [
  { label: 'Semua' },
  { label: 'Selesai', status: 'DONE' },
  { label: 'Gagal', status: 'FAILED' },
  { label: 'Tanpa log', status: 'NO_LOGS' },
  { label: 'Analisis AI', status: 'ANALYZING' },
  { label: 'Antre', status: 'QUEUED' },
]

export function JobsPage() {
  const { design } = useDesign()
  // In the triage design the list lives in the shell; this route is the overview.
  if (design === 'triage') return <TriageOverview />
  return <JobsBrowser bento={design === 'bento'} />
}

function JobsBrowser({ bento }: { bento: boolean }) {
  const { user } = useAuth()
  const envs = useEnvironments()
  const [params, setParams] = useSearchParams()
  const filters: JobFilters = {}
  for (const k of filterKeys) {
    const v = params.get(k)
    if (v) filters[k] = v
  }
  const [more, setMore] = useState(Boolean(filters.environment || filters.from || filters.to))
  // Cursor pagination: each page starts below the smallest ID seen so far.
  const [cursors, setCursors] = useState<number[]>([])
  const before = cursors.at(-1)
  const jobs = useJobs(filters, before)

  function apply(next: URLSearchParams) {
    setCursors([])
    setParams(next)
  }

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const next = new URLSearchParams()
    for (const k of filterKeys) {
      const v = String(form.get(k) ?? params.get(k) ?? '').trim()
      if (v) next.set(k, v)
    }
    apply(next)
  }

  function setStatus(status?: string) {
    const next = new URLSearchParams(params)
    if (status) next.set('status', status)
    else next.delete('status')
    apply(next)
  }

  const list = jobs.data ?? []
  const engineer = user?.role === 'engineer'
  return (
    <div className="grid gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className={cn('font-display font-semibold tracking-tight', bento ? 'text-3xl font-bold' : 'text-xl')}>Histori investigasi</h1>
          <p className="text-sm text-muted-foreground">{engineer ? 'Semua investigasi dari semua user.' : 'Investigasi yang kamu submit.'}</p>
        </div>
      </div>

      <form onSubmit={onSubmit} key={params.toString()} className="grid gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <label className={cn('flex min-w-[200px] flex-1 items-center gap-2 border bg-card px-3 text-muted-foreground focus-within:border-ring', bento ? 'rounded-full' : 'rounded-lg')}>
            <Search className="size-4" />
            <span className="sr-only">Transaction ID</span>
            <input
              name="transactionId"
              defaultValue={filters.transactionId}
              placeholder="Cari transaction ID, lalu Enter"
              className="min-w-0 flex-1 bg-transparent py-2 font-mono text-[13px] text-foreground outline-none placeholder:font-sans"
            />
          </label>
          <Button type="button" variant="outline" size="sm" aria-expanded={more} onClick={() => setMore((m) => !m)} className={cn(bento && 'rounded-full')}>
            <SlidersHorizontal />
            Filter
          </Button>
          {params.size > 0 && (
            <Button type="button" variant="ghost" size="sm" onClick={() => apply(new URLSearchParams())}>
              Reset
            </Button>
          )}
        </div>
        {more && (
          <div className="grid animate-rise gap-3 rounded-xl border bg-card p-3 sm:grid-cols-[1fr_1fr_1fr_auto] sm:items-end">
            <div className="grid gap-1.5">
              <Label htmlFor="f-env">Environment</Label>
              <Select id="f-env" name="environment" defaultValue={filters.environment ?? ''}>
                <option value="">Semua</option>
                {(envs.data?.environments ?? []).map((e) => (
                  <option key={e}>{e}</option>
                ))}
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="from">Dari</Label>
              <Input id="from" name="from" type="date" defaultValue={filters.from} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="to">Sampai</Label>
              <Input id="to" name="to" type="date" defaultValue={filters.to} />
            </div>
            <Button type="submit" size="sm">
              Terapkan
            </Button>
          </div>
        )}
        <div className="flex gap-1.5 overflow-x-auto [scrollbar-width:none]">
          {statusChips.map((c) => (
            <button
              key={c.label}
              type="button"
              aria-pressed={filters.status === c.status}
              onClick={() => setStatus(c.status)}
              className={cn(
                'shrink-0 rounded-full border px-3 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground',
                filters.status === c.status && 'border-foreground bg-foreground text-background hover:text-background',
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
      </form>

      {jobs.error && <Alert tone="danger">{jobs.error.message}</Alert>}
      {jobs.isPending ? (
        <div className="grid gap-2">
          {Array.from({ length: 5 }, (_, i) => (
            <div key={i} className="skeleton h-11 w-full" />
          ))}
        </div>
      ) : list.length === 0 ? (
        <p className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">Belum ada investigasi yang cocok.</p>
      ) : bento ? (
        <JobCards jobs={list} showUser={engineer} />
      ) : (
        <JobRows jobs={list} showUser={engineer} />
      )}

      <div className="flex gap-2">
        {cursors.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => setCursors((c) => c.slice(0, -1))}>
            ← Lebih baru
          </Button>
        )}
        {list.length === PAGE_SIZE && (
          <Button variant="outline" size="sm" onClick={() => setCursors((c) => [...c, list[list.length - 1].id])}>
            Lebih lama →
          </Button>
        )}
      </div>
    </div>
  )
}
