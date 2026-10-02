import { Check, X } from 'lucide-react'
import { formatDuration, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { jobSteps } from './pipeline'
import { isFinal, type JobStatus, type JobView } from './status'

/**
 * Live pipeline stepper. `orientation="auto"` lays out horizontally when its
 * container is wide and vertically when narrow.
 */
export function JobTimeline({ job, timeZone, orientation = 'auto' }: { job: JobView; timeZone: string; orientation?: 'auto' | 'vertical' }) {
  const s = job.status as JobStatus
  const steps = jobSteps(job)
  const auto = orientation === 'auto'
  return (
    <div className="@container grid gap-4">
      <ol className={cn('grid', auto && '@xl:grid-cols-5')}>
        {steps.map((step, i) => {
          const last = i === steps.length - 1
          const lineDone = step.state === 'done' && steps[i + 1]?.state !== 'idle'
          return (
            <li
              key={step.key}
              className={cn(
                'relative grid grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-x-3 pb-4 last:pb-0',
                auto && '@xl:grid-cols-1 @xl:grid-rows-[20px_auto_auto] @xl:items-start @xl:gap-y-1.5 @xl:pb-0',
              )}
            >
              {!last && (
                <span
                  aria-hidden
                  className={cn(
                    'absolute top-[22px] bottom-0.5 left-[9.25px] w-[1.5px] bg-border',
                    auto && '@xl:top-[9.25px] @xl:right-1.5 @xl:bottom-auto @xl:left-[26px] @xl:h-[1.5px] @xl:w-auto',
                  )}
                >
                  <span className={cn('absolute inset-0 origin-top bg-ok transition-transform duration-500', auto && '@xl:origin-left', lineDone ? 'scale-100' : 'scale-0')} />
                </span>
              )}
              <span
                className={cn(
                  'relative z-[1] grid size-5 place-items-center rounded-full border-[1.5px] bg-card text-white transition-colors',
                  step.state === 'done' && 'border-ok bg-ok',
                  step.state === 'fail' && 'border-bad bg-bad',
                  step.state === 'run' && 'border-info',
                  step.state === 'wait' && 'border-warn bg-warn-bg',
                  step.state === 'skip' && 'border-dashed',
                )}
              >
                {step.state === 'done' && <Check className="size-3" strokeWidth={3.2} />}
                {step.state === 'fail' && <X className="size-3" strokeWidth={3.2} />}
                {step.state === 'run' && <span className="absolute -inset-1 animate-spin rounded-full border-2 border-transparent border-t-info" />}
                {step.state === 'wait' && <span className="size-1.5 animate-pulse rounded-full bg-warn" />}
              </span>
              <span className={cn('text-sm font-medium', (step.state === 'idle' || step.state === 'skip') && 'text-muted-foreground')}>{step.label}</span>
              <span className="font-mono text-[11.5px] text-muted-foreground tabular-nums">
                {step.state === 'done' && step.at
                  ? formatTime(step.at, timeZone)
                  : step.state === 'run'
                    ? 'berjalan…'
                    : step.state === 'wait'
                      ? 'menunggu'
                      : step.state === 'skip'
                        ? 'dilewati'
                        : step.state === 'fail'
                          ? 'berhenti'
                          : ''}
              </span>
            </li>
          )
        })}
      </ol>
      <p className="text-xs text-muted-foreground">
        Durasi {formatDuration(job.startedAt ?? job.queuedAt, isFinal(s) ? job.finishedAt : null)}
        {job.startedAt && ` · antre ${formatDuration(job.queuedAt, job.startedAt)}`}
      </p>
    </div>
  )
}
