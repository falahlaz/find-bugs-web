import { Check, Copy, ExternalLink, Eye, EyeOff } from 'lucide-react'
import { useState } from 'react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import type { ToolResult } from './queries'

function CopyButton({ value, label }: { value: string; label: string }) {
  const [done, setDone] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setDone(true)
      setTimeout(() => setDone(false), 1500)
    } catch {
      /* clipboard unavailable (http or denied) */
    }
  }
  return (
    <Button type="button" variant="outline" size="sm" onClick={() => void copy()} aria-label={`Salin ${label}`}>
      {done ? <Check aria-hidden /> : <Copy aria-hidden />}
      {done ? 'Tersalin' : 'Salin'}
    </Button>
  )
}

function SecretValue({ value }: { value: string }) {
  const [shown, setShown] = useState(false)
  return (
    <span className="flex min-w-0 items-start gap-2">
      <span className="min-w-0 font-mono break-all">{shown ? value : '••••••••'}</span>
      <button type="button" onClick={() => setShown((s) => !s)} className="shrink-0 text-muted-foreground hover:text-foreground" aria-label={shown ? 'Sembunyikan' : 'Tampilkan'}>
        {shown ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </span>
  )
}

export function ToolResultView({ result }: { result: ToolResult }) {
  return (
    <div className="grid gap-4">
      {result.warnings.map((w) => (
        <Alert key={w} tone="warning">
          {w}
        </Alert>
      ))}
      {result.outputs.map((o, i) => (
        <section key={`${o.label}-${i}`} className="grid gap-1.5">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-xs font-medium text-muted-foreground">{o.label}</h3>
            <div className="flex gap-1.5">
              {o.kind === 'link' && (
                <a href={o.value} target="_blank" rel="noreferrer noopener" className="inline-flex h-8 items-center gap-1.5 rounded-md border bg-card px-3 text-[13px] font-medium shadow-xs hover:bg-secondary">
                  <ExternalLink className="size-4" aria-hidden />
                  Buka
                </a>
              )}
              <CopyButton value={o.value} label={o.label} />
            </div>
          </div>
          <pre className="max-h-96 overflow-auto rounded-lg border bg-muted px-3 py-2.5 font-mono text-[13px] leading-relaxed break-all whitespace-pre-wrap">{o.value}</pre>
        </section>
      ))}
      {result.summary.length > 0 && (
        <dl className="grid gap-x-4 gap-y-2 border-t pt-3 text-sm sm:grid-cols-[minmax(8rem,auto)_1fr]">
          {result.summary.map((s, i) => (
            <div key={`${s.label}-${i}`} className="contents">
              <dt className="text-xs font-medium text-muted-foreground sm:pt-0.5">{s.label}</dt>
              <dd className="min-w-0 max-sm:mb-1">{s.secret ? <SecretValue value={s.value} /> : <span className="font-mono break-all whitespace-pre-wrap">{s.value}</span>}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  )
}
