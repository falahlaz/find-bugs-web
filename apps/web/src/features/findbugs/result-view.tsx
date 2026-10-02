import { Alert } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { Schemas } from '@/lib/api'
import { SeverityBadge } from './status-badge'

type Result = Schemas['Result']

function Field({ label, value }: { label: string; value?: string }) {
  if (!value) return null
  return (
    <div className="grid gap-1">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="whitespace-pre-wrap break-words text-sm">{value}</div>
    </div>
  )
}

/** Diagnosis card. The API already strips engineer-only fields for QA users. */
export function ResultView({ result, engineer }: { result: Result; engineer: boolean }) {
  return (
    <div className="grid gap-4">
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center gap-2">
          <CardTitle>Hasil diagnosis</CardTitle>
          <SeverityBadge severity={result.severity} />
          {result.sourceLabel && <span className="text-sm text-muted-foreground">Sumber: {result.sourceLabel}</span>}
        </CardHeader>
        <CardContent className="grid gap-4">
          {result.llmFailed && (
            <Alert tone="warning">Analisis AI gagal untuk job ini. {engineer ? 'Log mentah di bawah tetap bisa dipakai.' : ''}</Alert>
          )}
          <Field label="Ringkasan" value={result.summary} />
          {result.qaMessage && <p className="text-sm text-muted-foreground">{result.qaMessage}</p>}
          {result.linkedIds && result.linkedIds.length > 0 && (
            <div className="grid gap-1">
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">ID backend terkait</div>
              <div className="flex flex-wrap gap-2">
                {result.linkedIds.map((id) => (
                  <code key={id} className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
                    {id}
                  </code>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                Service mencatat transaksi ini dengan ID sendiri; log dari ID tersebut ikut dianalisis.
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      {engineer && (
        <Card>
          <CardHeader>
            <CardTitle>Laporan engineer</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <Field label="Error type" value={result.errorType} />
            <Field label="Komponen gagal" value={result.failedComponent} />
            <Field label="Model AI" value={result.model} />
            <div className="sm:col-span-2">
              <Field label="Kemungkinan penyebab" value={result.likelyCause} />
            </div>
            <div className="sm:col-span-2">
              <Field label="Langkah selanjutnya" value={result.suggestedAction} />
            </div>
            {result.relevantLogs && result.relevantLogs.length > 0 && (
              <div className="grid gap-1 sm:col-span-2">
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Log relevan</div>
                <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{result.relevantLogs.join('\n')}</pre>
              </div>
            )}
            {result.rawLogSnippet && (
              <details className="sm:col-span-2">
                <summary className="cursor-pointer text-sm font-medium">Log mentah (3000 karakter terakhir, sudah diredaksi)</summary>
                <pre className="mt-2 max-h-96 overflow-auto rounded-md bg-muted p-3 text-xs">{result.rawLogSnippet}</pre>
              </details>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
