import { Link } from 'react-router'
import { formatShort } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useTimezone } from './queries'
import { jobTone, type Job, type JobStatus } from './status'
import { StatusBadge } from './status-badge'

const dot = { neutral: 'bg-neu', info: 'bg-info', success: 'bg-ok', warning: 'bg-warn', danger: 'bg-bad', outline: 'bg-border' } as const

/** A · Command: dense rows that fold into two lines on phones. */
export function JobRows({ jobs, showUser }: { jobs: Job[]; showUser: boolean }) {
  const tz = useTimezone()
  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      {jobs.map((j) => (
        <Link
          key={j.id}
          to={`/jobs/${j.id}`}
          className={cn(
            'grid items-center gap-x-3 gap-y-1 border-t px-3.5 py-2.5 text-[13px] transition-colors first:border-t-0 hover:bg-secondary',
            '[grid-template-areas:"tx_st"_"meta_time"] grid-cols-[minmax(0,1fr)_auto]',
            'md:[grid-template-areas:"id_tx_env_st_user_time"] md:grid-cols-[52px_minmax(0,1.6fr)_70px_150px_90px_90px] md:py-2',
          )}
        >
          <span className="font-mono text-xs text-muted-foreground max-md:hidden [grid-area:id]">#{j.id}</span>
          <span className="truncate font-mono text-[12.5px] [grid-area:tx]">{j.transactionId}</span>
          <span className="text-xs text-muted-foreground max-md:hidden [grid-area:env]">{j.environment}</span>
          <span className="justify-self-end [grid-area:st] md:justify-self-start">
            <StatusBadge status={j.status as JobStatus} />
          </span>
          <span className="truncate text-xs text-muted-foreground [grid-area:meta] md:[grid-area:user]">
            <span className="md:hidden">
              #{j.id} · {j.environment} · {j.timeRange}
              {showUser && ' · '}
            </span>
            {showUser && j.username}
          </span>
          <span className="text-right text-xs text-muted-foreground tabular-nums [grid-area:time]">{formatShort(j.queuedAt, tz)}</span>
        </Link>
      ))}
    </div>
  )
}

/** C · Bento: tappable cards in a fluid grid. */
export function JobCards({ jobs, showUser }: { jobs: Job[]; showUser: boolean }) {
  const tz = useTimezone()
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,230px),1fr))] gap-3">
      {jobs.map((j) => {
        const s = j.status as JobStatus
        return (
          <Link
            key={j.id}
            to={`/jobs/${j.id}`}
            className="group grid gap-3 rounded-xl border bg-card p-4 transition-[transform,box-shadow] duration-200 hover:-translate-y-0.5 hover:shadow-[0_14px_34px_-16px_rgb(20_30_80/0.35)]"
          >
            <span className="flex items-center justify-between gap-2">
              <StatusBadge status={s} />
              <span className="text-xs text-muted-foreground tabular-nums">{formatShort(j.queuedAt, tz)}</span>
            </span>
            <span className="font-mono text-[13px] font-medium break-all">{j.transactionId}</span>
            <span className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className={cn('size-1.5 rounded-full', dot[jobTone(s)])} aria-hidden />#{j.id} · {j.environment} · {j.timeRange}
              {showUser && ` · ${j.username}`}
            </span>
          </Link>
        )
      })}
    </div>
  )
}
