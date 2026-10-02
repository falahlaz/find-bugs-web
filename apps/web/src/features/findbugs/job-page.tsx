import { ChevronLeft, Link2, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { ApiError } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { notify } from '@/lib/notify'
import { JobTimeline } from './job-timeline'
import { useCancelJob, useJob, useTimezone } from './queries'
import { FieldLabel, ResultView, type Result } from './result-view'
import { isFinal, isPending, statusInfo, type JobStatus, type JobView } from './status'
import { SeverityBadge, StatusBadge } from './status-badge'
import { TraceSection } from './trace-chat'
import { useRerun } from './use-rerun'

function Notices({ job }: { job: JobView }) {
  const status = job.status as JobStatus
  const hint = statusInfo[status].hint
  return (
    <>
      {job.queuePosition != null && job.queuePosition > 0 && <Alert>Ada {job.queuePosition} job di depan job ini.</Alert>}
      {hint && (
        <Alert tone="warning">
          {hint}{' '}
          <Link className="font-medium underline" to={status === 'WAITING_VPN' ? '/koneksi#vpn' : '/koneksi#splunk'}>
            Buka panel
          </Link>
        </Alert>
      )}
      {job.failureReason && <Alert tone={status === 'DONE' ? 'warning' : status === 'CANCELLED' ? 'neutral' : 'danger'}>{job.failureReason}</Alert>}
      {status === 'NO_LOGS' && (
        <Alert tone="info">
          Tidak ada log untuk transaction ID ini di {job.environment} dalam {job.timeRange} terakhir. Cek environment-nya, atau coba rentang 48 jam.
        </Alert>
      )}
    </>
  )
}

function AnalyzingPlaceholder() {
  return (
    <div className="grid gap-2.5">
      <div className="skeleton h-3 w-[92%]" />
      <div className="skeleton h-3 w-[84%]" />
      <div className="skeleton h-3 w-[58%]" />
      <p className="text-xs text-muted-foreground">AI sedang membaca log…</p>
    </div>
  )
}

/** Keyed by ID so state (tabs, the finish animation) resets per job. */
export function JobPage() {
  const id = Number(useParams().id)
  return <JobDetail key={id} id={id} />
}

function JobDetail({ id }: { id: number }) {
  const { user } = useAuth()
  const tz = useTimezone()
  const job = useJob(id)
  const cancel = useCancelJob()
  const rerun = useRerun()
  const prevStatus = useRef<JobStatus | null>(null)
  const [justFinished, setJustFinished] = useState(false)

  const data = job.data
  const status = data?.status as JobStatus | undefined

  // Browser notification (and the typed-out summary) when a watched job finishes.
  useEffect(() => {
    if (!data || !status) return
    const prev = prevStatus.current
    prevStatus.current = status
    if (prev && !isFinal(prev) && isFinal(status)) {
      setJustFinished(true)
      notify(`Job #${data.id} ${statusInfo[status].label.toLowerCase()}`, `${data.transactionId} (${data.environment})`, `/jobs/${data.id}`)
    }
  }, [data, status])

  if (!Number.isInteger(id) || id <= 0) return <Alert tone="danger">ID job tidak valid.</Alert>
  if (job.isPending)
    return (
      <div className="grid gap-4">
        <div className="skeleton h-6 w-64" />
        <div className="skeleton h-28 w-full" />
      </div>
    )
  if (job.error) {
    const notFound = job.error instanceof ApiError && job.error.status === 404
    return <Alert tone="danger">{notFound ? 'Job tidak ditemukan atau bukan milikmu.' : job.error.message}</Alert>
  }
  if (!data || !status) return null

  const engineer = user?.role === 'engineer'
  const canCancel = isPending(status) && (engineer || data.userId === user?.id)
  const result: Result | null = data.result && status === 'DONE' ? data.result : null

  // Phones show one pane at a time, so the detail needs a way back to the list.
  const back = (
    <Link to="/jobs" className="inline-flex items-center gap-1 text-[13px] font-medium text-primary md:hidden">
      <ChevronLeft className="size-4" />
      Investigasi
    </Link>
  )

  const header = (
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="grid min-w-0 gap-1.5">
        {back}
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="font-mono text-lg font-medium break-all md:text-xl">{data.transactionId}</h1>
          <StatusBadge status={status} />
          {result && <SeverityBadge severity={result.severity} />}
        </div>
        <p className="text-[13px] text-muted-foreground">
          Job #{data.id} · {data.environment} · {data.timeRange} · input {data.inputKind === 'curl' ? 'curl' : 'transaction ID'} · oleh {data.username} ·{' '}
          {formatDateTime(data.queuedAt, tz)}
        </p>
      </div>
      <div className="flex gap-2">
        {canCancel && (
          <Button variant="outline" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(data.id)}>
            Batalkan
          </Button>
        )}
        {isFinal(status) && (
          <Button size="sm" disabled={rerun.isPending} onClick={() => rerun.mutate(data)}>
            <RefreshCw />
            Jalankan ulang
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={() => void navigator.clipboard?.writeText(location.href).catch(() => {})} title="Salin link job">
          <Link2 />
          <span className="max-sm:sr-only">Salin link</span>
        </Button>
      </div>
    </header>
  )
  const errors = (
    <>
      {cancel.error && <Alert tone="danger">{cancel.error.message}</Alert>}
      {rerun.error && <Alert tone="danger">{rerun.error.message}</Alert>}
    </>
  )

  return (
    <div className="grid gap-4">
      {header}
      <Notices job={data} />
      {errors}
      <div className="grid items-start gap-4 @3xl:grid-cols-[250px_minmax(0,1fr)]">
        <Card className="gap-3 py-4">
          <CardContent className="grid gap-3 px-4">
            <FieldLabel>Pipeline</FieldLabel>
            <JobTimeline job={data} timeZone={tz} orientation="vertical" />
          </CardContent>
        </Card>
        <div className="grid gap-4">
          {status === 'ANALYZING' && (
            <Card>
              <CardContent>
                <AnalyzingPlaceholder />
              </CardContent>
            </Card>
          )}
          {result && (
            <ResultView
              result={result}
              engineer={engineer}
              animate={justFinished}
              trace={engineer && result.codeTrace ? <TraceSection jobId={id} initial={result.codeTrace} /> : undefined}
            />
          )}
          {!result && status !== 'ANALYZING' && isFinal(status) === false && (
            <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">Diagnosis muncul di sini setelah analisis AI selesai.</p>
          )}
        </div>
      </div>
    </div>
  )
}
