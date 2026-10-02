import { ChevronLeft, Link2, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { useDesign } from '@/app/design'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { ApiError } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { notify } from '@/lib/notify'
import { cn } from '@/lib/utils'
import { JobTimeline } from './job-timeline'
import { jobProgress } from './pipeline'
import { useCancelJob, useJob, useTimezone } from './queries'
import { Diagnosis, EngineerLogs, EngineerReport, FieldLabel, ResultView, type Result } from './result-view'
import { isFinal, isPending, statusInfo, type JobStatus, type JobView } from './status'
import { SeverityBadge, StatusBadge } from './status-badge'
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

/** Small ring that fills as the pipeline advances (bento). */
function ProgressRing({ value, size = 120, label }: { value: number; size?: number; label: string }) {
  const r = size * 0.4
  const c = 2 * Math.PI * r
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="shrink-0" role="img" aria-label={label}>
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={size * 0.08} className="stroke-secondary" />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={r}
        fill="none"
        strokeWidth={size * 0.08}
        strokeLinecap="round"
        strokeDasharray={c}
        strokeDashoffset={c * (1 - value)}
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        className="stroke-primary transition-[stroke-dashoffset] duration-700 ease-out"
      />
      <text x="50%" y="50%" textAnchor="middle" dominantBaseline="central" className="fill-foreground font-display text-[22px] font-bold">
        {Math.round(value * 100)}%
      </text>
    </svg>
  )
}

const stageText: Record<JobStatus, string> = {
  QUEUED: 'Menunggu giliran',
  WAITING_VPN: 'Menunggu VPN',
  CHECKING_VPN: 'Cek VPN',
  WAITING_SPLUNK: 'Menunggu Splunk',
  SEARCHING: 'Mencari log di Splunk',
  ANALYZING: 'AI sedang menganalisis',
  DONE: 'Diagnosis siap',
  NO_LOGS: 'Log tidak ditemukan',
  FAILED: 'Gagal',
  CANCELLED: 'Dibatalkan',
  EXPIRED: 'Kedaluwarsa',
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

function Tile({ className, children }: { className?: string; children: ReactNode }) {
  return <section className={cn('flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-5', className)}>{children}</section>
}

/** Keyed by ID so state (tabs, the finish animation) resets per job. */
export function JobPage() {
  const id = Number(useParams().id)
  return <JobDetail key={id} id={id} />
}

function JobDetail({ id }: { id: number }) {
  const { user } = useAuth()
  const { design } = useDesign()
  const tz = useTimezone()
  const job = useJob(id)
  const cancel = useCancelJob()
  const rerun = useRerun()
  const prevStatus = useRef<JobStatus | null>(null)
  const [justFinished, setJustFinished] = useState(false)
  const [tab, setTab] = useState<'diag' | 'eng' | 'log'>('diag')

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

  const back =
    design === 'command' ? null : (
      <Link to="/jobs" className={cn('inline-flex items-center gap-1 text-[13px] font-medium text-muted-foreground hover:text-foreground', design === 'triage' && 'text-primary md:hidden')}>
        <ChevronLeft className="size-4" />
        {design === 'triage' ? 'Investigasi' : 'Histori'}
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
          <Button variant={design === 'command' ? 'outline' : 'default'} size="sm" disabled={rerun.isPending} onClick={() => rerun.mutate(data)}>
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

  if (design === 'triage') {
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
            {result && <ResultView result={result} engineer={engineer} animate={justFinished} />}
            {!result && status !== 'ANALYZING' && isFinal(status) === false && (
              <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">Diagnosis muncul di sini setelah analisis AI selesai.</p>
            )}
          </div>
        </div>
      </div>
    )
  }

  if (design === 'bento') {
    return (
      <div className="grid gap-3">
        {header}
        <Notices job={data} />
        {errors}
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          <Tile className="md:row-span-2 lg:col-span-2">
            <div className="flex items-center justify-between text-xs font-medium text-muted-foreground">
              <span>Status</span>
              <StatusBadge status={status} />
            </div>
            <div className="flex flex-wrap items-center gap-5">
              <ProgressRing value={jobProgress(data)} label="Progres pipeline" />
              <div className="grid min-w-[150px] flex-1 gap-1">
                <span className="font-display text-2xl leading-tight font-bold tracking-tight">{stageText[status]}</span>
                <span className="text-xs text-muted-foreground">{data.environment} · rentang {data.timeRange}</span>
              </div>
            </div>
            <JobTimeline job={data} timeZone={tz} orientation="vertical" />
          </Tile>
          <Tile className="md:row-span-2 lg:col-span-2">
            <div className="flex items-center justify-between text-xs font-medium text-muted-foreground">
              <span>Diagnosis AI</span>
              {result && <SeverityBadge severity={result.severity} />}
            </div>
            {result ? (
              <>
                <Diagnosis key={String(justFinished)} result={result} engineer={engineer} animate={justFinished} header={false} />
                {result.severity && <SeverityMeter severity={result.severity} />}
              </>
            ) : status === 'ANALYZING' ? (
              <AnalyzingPlaceholder />
            ) : (
              <p className="text-sm text-muted-foreground">{isFinal(status) ? 'Tidak ada diagnosis untuk job ini.' : 'Diagnosis muncul di sini setelah analisis AI selesai.'}</p>
            )}
          </Tile>
          {result && engineer && (
            <>
              <Tile>
                <FieldLabel>Error type</FieldLabel>
                <p className="font-mono text-sm font-medium break-words">{result.errorType || '–'}</p>
              </Tile>
              <Tile>
                <FieldLabel>Komponen gagal</FieldLabel>
                <p className="font-mono text-sm font-medium break-words">{result.failedComponent || '–'}</p>
              </Tile>
              <Tile className="lg:col-span-2">
                <FieldLabel>Kemungkinan penyebab</FieldLabel>
                <p className="text-sm">{result.likelyCause || '–'}</p>
              </Tile>
              <Tile className="md:col-span-2 lg:col-span-4">
                <FieldLabel>Langkah selanjutnya</FieldLabel>
                <p className="text-sm">{result.suggestedAction || '–'}</p>
              </Tile>
              <Tile className="md:col-span-2 lg:col-span-4">
                <FieldLabel>Log</FieldLabel>
                <EngineerLogs result={result} />
              </Tile>
            </>
          )}
        </div>
      </div>
    )
  }

  // A · Command
  const tabs = [
    { id: 'diag' as const, label: 'Diagnosis' },
    ...(engineer ? [{ id: 'eng' as const, label: 'Laporan engineer' }, { id: 'log' as const, label: 'Log', count: result?.relevantLogs?.length }] : []),
  ]
  return (
    <div className="grid gap-5">
      {header}
      <Card className="py-5">
        <CardContent>
          <JobTimeline job={data} timeZone={tz} />
        </CardContent>
      </Card>
      <Notices job={data} />
      {errors}
      {status === 'ANALYZING' && (
        <Card>
          <CardContent>
            <AnalyzingPlaceholder />
          </CardContent>
        </Card>
      )}
      {result && (
        <section className="grid gap-4">
          <div role="tablist" className="flex gap-1 overflow-x-auto border-b [scrollbar-width:none]">
            {tabs.map((t) => (
              <button
                key={t.id}
                role="tab"
                type="button"
                aria-selected={tab === t.id}
                onClick={() => setTab(t.id)}
                className={cn(
                  'relative px-3 py-2 text-[13px] whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground',
                  tab === t.id && 'font-medium text-foreground after:absolute after:inset-x-2 after:-bottom-px after:h-0.5 after:rounded-full after:bg-foreground',
                )}
              >
                {t.label}
                {'count' in t && t.count ? <span className="ml-1.5 text-[11px] text-muted-foreground">{t.count}</span> : null}
              </button>
            ))}
          </div>
          {tab === 'diag' && <Diagnosis key={String(justFinished)} result={result} engineer={engineer} animate={justFinished} />}
          {tab === 'eng' && <EngineerReport result={result} />}
          {tab === 'log' && <EngineerLogs result={result} />}
        </section>
      )}
    </div>
  )
}

function SeverityMeter({ severity }: { severity: string }) {
  const level = { low: 1, medium: 2, high: 3, critical: 4 }[severity] ?? 0
  const color = level >= 3 ? 'bg-bad' : level === 2 ? 'bg-warn' : 'bg-ok'
  return (
    <div className="mt-auto grid gap-1.5">
      <FieldLabel>Severity</FieldLabel>
      <div className="grid max-w-52 grid-cols-4 gap-1" role="img" aria-label={`Severity ${severity}`}>
        {[1, 2, 3, 4].map((i) => (
          <span key={i} className={cn('h-2 rounded-full', i <= level ? color : 'bg-secondary')} />
        ))}
      </div>
    </div>
  )
}
