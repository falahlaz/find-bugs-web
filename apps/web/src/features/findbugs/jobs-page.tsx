import { useAuth } from '@/app/auth-context'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { useJobs } from './queries'
import { isFinal, isTrouble, type JobStatus } from './status'

/**
 * /jobs: the job list lives in the shell, so this route is the overview the
 * detail pane shows before a job is picked.
 */
export function JobsPage() {
  const { user } = useAuth()
  const jobs = useJobs(user?.role === 'engineer' ? {} : { mine: '1' })
  const list = jobs.data ?? []
  const st = (j: (typeof list)[number]) => j.status as JobStatus
  const stats = [
    { label: 'Job terakhir', value: list.length, dot: 'bg-neu' },
    { label: 'Selesai', value: list.filter((j) => st(j) === 'DONE').length, dot: 'bg-ok' },
    { label: 'Gagal / kedaluwarsa', value: list.filter((j) => isTrouble(st(j))).length, dot: 'bg-bad' },
    { label: 'Berjalan', value: list.filter((j) => !isFinal(st(j))).length, dot: 'bg-info' },
  ]
  const byEnv = Object.entries(
    list.reduce<Record<string, number>>((acc, j) => {
      acc[j.environment] = (acc[j.environment] ?? 0) + 1
      return acc
    }, {}),
  ).sort((a, b) => b[1] - a[1])
  const max = Math.max(1, ...byEnv.map(([, n]) => n))

  return (
    <div className="grid gap-5">
      <PageHeader title="Ringkasan" description={`Dari ${list.length} job terakhir. Pilih job di daftar untuk melihat detailnya.`} />
      <div className="grid grid-cols-2 gap-3 @2xl:grid-cols-4">
        {stats.map((s) => (
          <div key={s.label} className="grid gap-1 rounded-lg border bg-card px-4 py-3">
            <span className="text-2xl font-semibold tabular-nums">{jobs.isPending ? '–' : s.value}</span>
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <span className={`size-2 rounded-full ${s.dot}`} aria-hidden />
              {s.label}
            </span>
          </div>
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">Job per environment</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3">
          {byEnv.length === 0 && <p className="text-sm text-muted-foreground">Belum ada job.</p>}
          {byEnv.map(([env, n]) => (
            <div key={env} className="grid grid-cols-[110px_minmax(0,1fr)_32px] items-center gap-3 text-[13px]">
              <span className="truncate font-mono text-xs">{env}</span>
              <span className="h-2 overflow-hidden rounded-full bg-secondary">
                <span className="block h-full rounded-full bg-primary transition-[width] duration-500" style={{ width: `${(n / max) * 100}%` }} />
              </span>
              <span className="text-right text-muted-foreground tabular-nums">{n}</span>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
