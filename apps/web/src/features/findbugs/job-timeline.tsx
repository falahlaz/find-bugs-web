import { formatDuration, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { isFinal, type JobStatus, type JobView } from './status'

type Step = { key: string; label: string; at?: string | null; active: boolean }

/** Shows the pipeline stages with the time each one finished. */
export function JobTimeline({ job, timeZone }: { job: JobView; timeZone: string }) {
  const s = job.status as JobStatus
  const steps: Step[] = [
    { key: 'queued', label: 'Masuk antrean', at: job.queuedAt, active: s === 'QUEUED' || s === 'WAITING_VPN' || s === 'WAITING_SPLUNK' },
    { key: 'vpn', label: 'Cek VPN', at: job.vpnCheckedAt, active: s === 'CHECKING_VPN' },
    { key: 'search', label: 'Cari log di Splunk', at: job.searchDoneAt, active: s === 'SEARCHING' },
    { key: 'analyze', label: 'Analisis AI', at: job.analyzedAt, active: s === 'ANALYZING' },
    { key: 'done', label: 'Selesai', at: job.finishedAt, active: false },
  ]
  return (
    <ol className="grid gap-3 sm:grid-cols-5">
      {steps.map((step) => {
        const done = Boolean(step.at)
        return (
          <li key={step.key} className="flex items-start gap-2 sm:flex-col">
            <span
              className={cn(
                'mt-1 size-3 shrink-0 rounded-full border-2',
                done && 'border-emerald-500 bg-emerald-500',
                step.active && 'animate-pulse border-sky-500 bg-sky-200 dark:bg-sky-900',
                !done && !step.active && 'border-muted-foreground/40',
              )}
            />
            <div className="text-sm">
              <div className={cn(step.active ? 'font-medium' : done ? '' : 'text-muted-foreground')}>{step.label}</div>
              <div className="text-xs text-muted-foreground">{done ? formatTime(step.at, timeZone) : step.active ? 'berjalan…' : ''}</div>
            </div>
          </li>
        )
      })}
      <li className="text-xs text-muted-foreground sm:col-span-5">
        Durasi: {formatDuration(job.startedAt ?? job.queuedAt, isFinal(s) ? job.finishedAt : null)}
        {job.startedAt && ` (antre ${formatDuration(job.queuedAt, job.startedAt)})`}
      </li>
    </ol>
  )
}
