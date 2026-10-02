import type { JobStatus, JobView } from './status'

export type StepState = 'done' | 'run' | 'wait' | 'fail' | 'skip' | 'idle'
type Step = { key: string; label: string; at?: string | null; state: StepState }

const failed: JobStatus[] = ['FAILED', 'CANCELLED', 'EXPIRED']

/** Pipeline stages with a state each, derived from the job's timestamps. */
export function jobSteps(job: JobView): Step[] {
  const s = job.status as JobStatus
  const raw = [
    { key: 'queued', label: 'Masuk antrean', at: job.queuedAt },
    { key: 'vpn', label: 'Cek VPN', at: job.vpnCheckedAt },
    { key: 'search', label: 'Cari log di Splunk', at: job.searchDoneAt },
    { key: 'analyze', label: 'Analisis AI', at: job.analyzedAt },
    { key: 'done', label: 'Selesai', at: job.finishedAt },
  ]
  const current: Partial<Record<JobStatus, string>> = {
    QUEUED: 'vpn',
    WAITING_VPN: 'vpn',
    CHECKING_VPN: 'vpn',
    WAITING_SPLUNK: 'search',
    SEARCHING: 'search',
    ANALYZING: 'analyze',
  }
  // The first stage without a timestamp is where a failed job stopped.
  const stoppedAt = failed.includes(s) ? raw.findIndex((r, i) => i > 0 && i < 4 && !r.at) : -1
  return raw.map((r, i) => {
    let state: StepState = 'idle'
    if (r.at && (r.key !== 'done' || s === 'DONE' || s === 'NO_LOGS')) state = 'done'
    else if (s === 'NO_LOGS' && r.key === 'analyze') state = 'skip'
    else if (i === stoppedAt) state = 'fail'
    else if (current[s] === r.key) state = s.startsWith('WAITING') || s === 'QUEUED' ? 'wait' : 'run'
    if (r.key === 'queued') state = 'done'
    return { ...r, state }
  })
}

/** 0..1 share of the pipeline that is behind this job. */
export function jobProgress(job: JobView) {
  const steps = jobSteps(job)
  return steps.filter((s) => s.state === 'done' || s.state === 'skip').length / steps.length
}
