import { useEffect, useRef } from 'react'
import { Link, useParams } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ApiError } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { notify } from '@/lib/notify'
import { JobTimeline } from './job-timeline'
import { useCancelJob, useJob, useTimezone } from './queries'
import { ResultView } from './result-view'
import { isFinal, isPending, statusInfo, type JobStatus } from './status'
import { StatusBadge } from './status-badge'
import { useRerun } from './use-rerun'

export function JobPage() {
  const id = Number(useParams().id)
  const { user } = useAuth()
  const tz = useTimezone()
  const job = useJob(id)
  const cancel = useCancelJob()
  const rerun = useRerun()
  const prevStatus = useRef<JobStatus | null>(null)

  const data = job.data
  const status = data?.status as JobStatus | undefined

  // Browser notification when a job we watched finishes.
  useEffect(() => {
    if (!data || !status) return
    const prev = prevStatus.current
    prevStatus.current = status
    if (prev && !isFinal(prev) && isFinal(status)) {
      notify(`Job #${data.id} ${statusInfo[status].label.toLowerCase()}`, `${data.transactionId} (${data.environment})`, `/jobs/${data.id}`)
    }
  }, [data, status])

  if (!Number.isInteger(id) || id <= 0) return <Alert tone="danger">ID job tidak valid.</Alert>
  if (job.isPending) return <p className="text-muted-foreground">Memuat job…</p>
  if (job.error) {
    const notFound = job.error instanceof ApiError && job.error.status === 404
    return <Alert tone="danger">{notFound ? 'Job tidak ditemukan atau bukan milikmu.' : job.error.message}</Alert>
  }
  if (!data || !status) return null

  const engineer = user?.role === 'engineer'
  const canCancel = isPending(status) && (engineer || data.userId === user?.id)
  const hint = statusInfo[status].hint

  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-4">
          <div className="grid gap-1.5">
            <CardTitle className="flex flex-wrap items-center gap-2">
              <span className="font-mono">{data.transactionId}</span>
              <StatusBadge status={status} />
            </CardTitle>
            <CardDescription>
              Job #{data.id} · {data.environment} · {data.timeRange} · input {data.inputKind === 'curl' ? 'curl' : 'transaction ID'} · oleh{' '}
              {data.username} · {formatDateTime(data.queuedAt, tz)}
            </CardDescription>
          </div>
          <div className="flex gap-2">
            {canCancel && (
              <Button variant="outline" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(data.id)}>
                Batalkan
              </Button>
            )}
            {isFinal(status) && (
              <Button variant="outline" size="sm" disabled={rerun.isPending} onClick={() => rerun.mutate(data)}>
                Jalankan ulang
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent className="grid gap-4">
          <JobTimeline job={data} timeZone={tz} />
          {data.queuePosition != null && data.queuePosition > 0 && (
            <p className="text-sm text-muted-foreground">Ada {data.queuePosition} job di depan job ini.</p>
          )}
          {hint && (
            <Alert tone="warning">
              {hint}{' '}
              <Link className="font-medium underline" to={status === 'WAITING_VPN' ? '/koneksi#vpn' : '/koneksi#splunk'}>
                Buka panel
              </Link>
            </Alert>
          )}
          {data.failureReason && (
            <Alert tone={status === 'DONE' ? 'warning' : status === 'CANCELLED' ? 'neutral' : 'danger'}>{data.failureReason}</Alert>
          )}
          {status === 'NO_LOGS' && (
            <Alert tone="info">
              Tidak ada log untuk transaction ID ini di {data.environment} dalam {data.timeRange} terakhir. Cek environment-nya, atau coba rentang
              48 jam.
            </Alert>
          )}
          {cancel.error && <Alert tone="danger">{cancel.error.message}</Alert>}
          {rerun.error && <Alert tone="danger">{rerun.error.message}</Alert>}
        </CardContent>
      </Card>
      {data.result && status === 'DONE' && <ResultView result={data.result} engineer={engineer} />}
    </div>
  )
}
