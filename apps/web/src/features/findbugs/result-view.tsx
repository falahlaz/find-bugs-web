import { Code2, ExternalLink, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { Schemas } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { LogViewer } from './log-viewer'
import { Markdown } from './markdown'
import { SeverityBadge } from './status-badge'
import { useTypewriter } from './use-typewriter'

export type Result = Schemas['Result']

export function FieldLabel({ children }: { children: ReactNode }) {
  return <div className="text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">{children}</div>
}

function Field({ label, value, mono = false, className }: { label: string; value?: string; mono?: boolean; className?: string }) {
  if (!value) return null
  return (
    <div className={cn('grid gap-1', className)}>
      <FieldLabel>{label}</FieldLabel>
      <div className={cn('text-sm whitespace-pre-wrap [overflow-wrap:anywhere]', mono && 'font-mono text-[13px]')}>{value}</div>
    </div>
  )
}

/**
 * The AI summary, QA guidance and linked IDs. `animate` writes the summary
 * out when the result has just arrived on screen.
 */
export function Diagnosis({ result, engineer, animate = false }: { result: Result; engineer: boolean; animate?: boolean }) {
  const summary = useTypewriter(result.summary ?? '', animate)
  return (
    <div className="grid gap-3.5">
      <div className="flex flex-wrap items-center gap-2">
          <Sparkles className="size-4 text-primary" />
          <span className="font-semibold">Diagnosis AI</span>
          <SeverityBadge severity={result.severity} />
          {result.sourceLabel && <span className="text-xs text-muted-foreground">Sumber: {result.sourceLabel}</span>}
          {result.model && <span className="ml-auto rounded-md border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">{result.model}</span>}
      </div>
      {result.llmFailed && <Alert tone="warning">Analisis AI gagal untuk job ini. {engineer ? 'Log mentah di bawah tetap bisa dipakai.' : ''}</Alert>}
      {result.summary && <p className={cn('max-w-[68ch] text-[15px] leading-relaxed text-pretty', !summary.done && 'caret')}>{summary.text}</p>}
      {summary.done && result.qaMessage && <p className="animate-rise rounded-lg bg-secondary px-3 py-2.5 text-[13px]">{result.qaMessage}</p>}
      {summary.done && result.linkedIds && result.linkedIds.length > 0 && (
        <div className="grid animate-rise gap-1.5">
          <FieldLabel>ID backend terkait</FieldLabel>
          <div className="flex flex-wrap gap-1.5">
            {result.linkedIds.map((id) => (
              <code key={id} className="rounded-md border bg-muted px-1.5 py-0.5 text-xs">
                {id}
              </code>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">Service mencatat transaksi ini dengan ID sendiri; log dari ID tersebut ikut dianalisis.</p>
        </div>
      )}
    </div>
  )
}

/** Engineer-only fields. The API already strips them for QA users. */
export function EngineerReport({ result, className }: { result: Result; className?: string }) {
  return (
    <dl className={cn('grid gap-4 sm:grid-cols-2', className)}>
      <Field label="Error type" value={result.errorType} mono />
      <Field label="Komponen gagal" value={result.failedComponent} mono />
      <Field label="Kemungkinan penyebab" value={result.likelyCause} className="sm:col-span-2" />
      <Field label="Langkah selanjutnya" value={result.suggestedAction} className="sm:col-span-2" />
    </dl>
  )
}

export function EngineerLogs({ result }: { result: Result }) {
  return (
    <div className="grid gap-3">
      {result.relevantLogs && result.relevantLogs.length > 0 ? (
        <LogViewer lines={result.relevantLogs} />
      ) : (
        <p className="text-sm text-muted-foreground">AI tidak menandai baris log tertentu.</p>
      )}
      {result.rawLogSnippet && (
        <details className="group">
          <summary className="cursor-pointer text-sm font-medium">Log mentah (3000 karakter terakhir, sudah diredaksi)</summary>
          <LogViewer className="mt-2" lines={result.rawLogSnippet.split('\n')} />
        </details>
      )}
    </div>
  )
}

type CodeTrace = NonNullable<Result['codeTrace']>

/** Where an internal error was traced to in the service code. */
export function CodeTraceView({ trace }: { trace: CodeTrace }) {
  if (trace.status === 'skipped' || trace.status === 'failed') {
    return <Alert tone={trace.status === 'failed' ? 'warning' : 'neutral'}>{trace.reason}</Alert>
  }
  const location = trace.file ? `${trace.file}${trace.line ? `:${trace.line}` : ''}` : ''
  return (
    <div className="grid grid-cols-1 gap-4">
      {trace.status === 'not_found' && <Alert tone="neutral">AI tidak menemukan lokasi kode yang cocok dengan yakin.</Alert>}
      {location && (
        <div className="grid gap-1">
          <FieldLabel>Lokasi</FieldLabel>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[13px] break-all">
            {trace.url ? (
              <a href={trace.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
                {location}
                <ExternalLink className="size-3.5 shrink-0" aria-hidden />
              </a>
            ) : (
              <span>{location}</span>
            )}
            {trace.function && <span className="text-muted-foreground">· {trace.function}</span>}
          </div>
        </div>
      )}
      {trace.snippet && (
        <pre className="max-h-96 overflow-auto rounded-lg border bg-muted px-3 py-2.5 font-mono text-[12.5px] leading-relaxed">{trace.snippet}</pre>
      )}
      {trace.explanation && (
        <div className="grid gap-1">
          <FieldLabel>Penjelasan</FieldLabel>
          <div className="text-sm leading-relaxed">
            <Markdown text={trace.explanation} />
          </div>
        </div>
      )}
      <div className="grid gap-1.5">
        <TraceVersion trace={trace} />
        {trace.config && <ConfigVersionLine config={trace.config} />}
      </div>
    </div>
  )
}

type ConfigVersion = NonNullable<CodeTrace['config']>

/** Which runtime JSON config (ConfigMaps) the trace could read. */
function ConfigVersionLine({ config }: { config: ConfigVersion }) {
  if (config.error || !config.commit) {
    return (
      <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
        <Badge tone="warning">Config server tidak terbaca</Badge>
        <span className="[overflow-wrap:anywhere]">{config.error}</span>
      </p>
    )
  }
  const commit = config.url ? (
    <a href={config.url} target="_blank" rel="noreferrer" className="font-mono text-primary hover:underline">
      {config.commit.slice(0, 8)}
    </a>
  ) : (
    <span className="font-mono">{config.commit.slice(0, 8)}</span>
  )
  const deployed = config.source === 'deployed'
  return (
    <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
      <Badge tone={deployed ? 'success' : 'warning'}>
        {deployed ? 'Config ter-deploy' : 'Config'} · {config.env}
      </Badge>
      <span className="[overflow-wrap:anywhere]">
        {config.project}/{config.path} @ {commit} dari branch <span className="font-mono">{config.branch}</span>
        {deployed && config.deployedAt && (
          <>
            , deploy{' '}
            {config.deployJobUrl ? (
              <a href={config.deployJobUrl} target="_blank" rel="noreferrer" className="text-primary hover:underline">
                {formatDateTime(config.deployedAt)}
              </a>
            ) : (
              formatDateTime(config.deployedAt)
            )}
          </>
        )}
        .{config.note && ` ${config.note}`}
      </span>
    </p>
  )
}

/** Which version of the code the trace read, and how it was picked. */
export function TraceVersion({ trace }: { trace: CodeTrace }) {
  if (!trace.commit) return null
  const commit = <span className="font-mono">{trace.commit.slice(0, 8)}</span>
  const ref = <span className="font-mono break-all">{trace.ref}</span>
  if (trace.refSource === 'deployed') {
    return (
      <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
        <Badge tone="success">Versi ter-deploy · {trace.env}</Badge>
        {trace.project} @ {commit} dari {ref}
        {trace.deployedAt && (
          <>
            , deploy{' '}
            {trace.deployJobUrl ? (
              <a href={trace.deployJobUrl} target="_blank" rel="noreferrer" className="text-primary hover:underline">
                {formatDateTime(trace.deployedAt)}
              </a>
            ) : (
              formatDateTime(trace.deployedAt)
            )}
          </>
        )}
        .
      </p>
    )
  }
  return (
    <p className="text-xs text-muted-foreground">
      {trace.project} @ {commit} dari {trace.refSource === 'manual' ? 'pilihan engineer' : 'branch'} {ref}
      {trace.refSource === 'manual' ? '. ' : ', bukan versi yang ter-deploy, jadi nomor baris bisa meleset. '}
      {trace.refNote}
    </p>
  )
}

/** Diagnosis plus the engineer report as stacked cards. */
export function ResultView({
  result,
  engineer,
  animate = false,
  trace,
}: {
  result: Result
  engineer: boolean
  animate?: boolean
  /** Replaces the plain code trace view (the job page adds the chat). */
  trace?: ReactNode
}) {
  return (
    <div className="grid gap-4">
      <Card>
        <CardContent>
          <Diagnosis key={String(animate)} result={result} engineer={engineer} animate={animate} />
        </CardContent>
      </Card>
      {engineer && (
        <Card>
          <CardHeader>
            <CardTitle>Laporan engineer</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-5">
            <EngineerReport result={result} />
            <EngineerLogs result={result} />
          </CardContent>
        </Card>
      )}
      {engineer && result.codeTrace && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Code2 className="size-4 text-primary" aria-hidden />
              Trace ke kode
            </CardTitle>
          </CardHeader>
          <CardContent>
            {trace ?? <CodeTraceView trace={result.codeTrace} />}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
