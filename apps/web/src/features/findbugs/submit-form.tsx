import { Check, CircleAlert, CornerDownLeft } from 'lucide-react'
import { useState, type FormEvent, type KeyboardEvent } from 'react'
import { useNavigate } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { LinkButton } from '@/components/ui/link-button'
import { Select } from '@/components/ui/select'
import { formatDateTime } from '@/lib/format'
import { requestNotificationPermission } from '@/lib/notify'
import { cn } from '@/lib/utils'
import { useEnvironments, useSubmitJob, useTimezone } from './queries'
import type { JobView } from './status'

const LAST_ENV_KEY = 'findbugs.lastEnvironment'

function readLastEnv() {
  try {
    return localStorage.getItem(LAST_ENV_KEY) ?? ''
  } catch {
    return ''
  }
}

/** Client-side hint only; the server does the real parsing. header is the configured transaction ID header. */
function detect(input: string, header: string): { tone: 'ok' | 'warn' | 'idle'; text: string; id?: string } {
  const v = input.trim()
  if (!v) return { tone: 'idle', text: `Satu transaction ID, atau satu perintah curl dengan header ${header}.` }
  if (/^curl\s/i.test(v)) {
    const name = header.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = v.match(new RegExp(`(?<![\\w-])${name}:\\s*([^\\s"'\\\\]+)`, 'i'))
    return m ? { tone: 'ok', text: `curl terdeteksi, ID dari header ${header}`, id: m[1] } : { tone: 'warn', text: `curl terdeteksi, tapi header ${header} tidak terlihat.` }
  }
  if (/\s/.test(v)) return { tone: 'warn', text: 'Ada spasi. Paste satu transaction ID atau satu perintah curl.' }
  return { tone: 'ok', text: 'Transaction ID', id: v }
}

function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-lg border bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn('rounded-md px-3 py-1 text-xs font-medium text-muted-foreground transition-colors', value === o.value && 'bg-card text-foreground shadow-xs')}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/**
 * The investigation composer: input with live detection, environment and
 * time range, duplicate handling, Ctrl+Enter to send.
 */
export function SubmitForm() {
  const navigate = useNavigate()
  const envs = useEnvironments()
  const submit = useSubmitJob()
  const tz = useTimezone()
  const [environment, setEnvironment] = useState(readLastEnv)
  const [timeRange, setTimeRange] = useState<'24h' | '48h'>('24h')
  const [input, setInput] = useState('')
  const [duplicate, setDuplicate] = useState<JobView | null>(null)

  // Fall back to the first environment when the remembered one is unknown.
  const envList = envs.data?.environments ?? []
  const env = envList.includes(environment) ? environment : (envList[0] ?? '')
  const header = envs.data?.transactionIdHeader ?? 'X-Transaction-ID'
  const hint = detect(input, header)

  async function send(force: boolean) {
    requestNotificationPermission()
    try {
      localStorage.setItem(LAST_ENV_KEY, env)
    } catch {
      /* storage unavailable */
    }
    const res = await submit.mutateAsync({ environment: env, timeRange, input, force })
    if (res.duplicate) {
      setDuplicate(res.job)
      return
    }
    navigate(`/jobs/${res.job.id}`)
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    setDuplicate(null)
    void send(false).catch(() => {})
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      e.currentTarget.form?.requestSubmit()
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-3">
      <label htmlFor="submit-input" className="text-xs font-medium text-muted-foreground">
        Transaction ID atau curl
      </label>
      <textarea
        id="submit-input"
        rows={6}
        spellCheck={false}
        required
        value={input}
        onChange={(e) => {
          setInput(e.target.value)
          setDuplicate(null)
        }}
        onKeyDown={onKeyDown}
        placeholder={`TRX-8F3A21C0-7B\natau curl -H "${header}: …" https://…`}
        className="w-full resize-y rounded-lg border border-input bg-card px-3 py-2.5 font-mono text-[13px] leading-relaxed outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40"
      />
      <p aria-live="polite" className="flex min-h-5 items-center gap-1.5 text-xs text-muted-foreground">
        {hint.tone === 'ok' && <Check className="size-3.5 shrink-0 text-ok" />}
        {hint.tone === 'warn' && <CircleAlert className="size-3.5 shrink-0 text-warn" />}
        <span>{hint.text}</span>
        {hint.id && <b className="truncate font-mono font-medium text-foreground">{hint.id}</b>}
      </p>

      {(submit.error || duplicate) && (
        <div className="grid gap-2">
          {submit.error && <Alert tone="danger">{submit.error.message}</Alert>}
          {duplicate && (
            <Alert tone="info">
              <div className="grid gap-2">
                <span>
                  <b className="font-mono">{duplicate.transactionId}</b> sudah diinvestigasi pada {formatDateTime(duplicate.queuedAt, tz)} dengan parameter yang sama.
                </span>
                <span className="flex flex-wrap gap-2">
                  <LinkButton size="sm" to={`/jobs/${duplicate.id}`}>
                    Lihat hasil sebelumnya
                  </LinkButton>
                  <Button type="button" variant="outline" size="sm" disabled={submit.isPending} onClick={() => void send(true).catch(() => {})}>
                    Jalankan ulang
                  </Button>
                </span>
              </div>
            </Alert>
          )}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {envList.length <= 4 ? (
          <Segmented label="Environment" value={env} options={envList.map((e) => ({ value: e, label: e }))} onChange={setEnvironment} />
        ) : (
          <Select aria-label="Environment" className="h-8 w-auto" value={env} onChange={(e) => setEnvironment(e.target.value)}>
            {envList.map((e) => (
              <option key={e}>{e}</option>
            ))}
          </Select>
        )}
        <Segmented<'24h' | '48h'>
          label="Rentang waktu"
          value={timeRange}
          options={[
            { value: '24h', label: '24 jam' },
            { value: '48h', label: '48 jam' },
          ]}
          onChange={setTimeRange}
        />
        <Button type="submit" size="sm" disabled={submit.isPending || !env} className="ml-auto">
          {submit.isPending ? 'Mengirim…' : 'Investigasi'}
          <kbd className="hidden rounded bg-white/15 px-1 font-mono text-[10px] sm:inline-flex sm:items-center sm:gap-0.5">
            Ctrl <CornerDownLeft className="!size-2.5" />
          </kbd>
        </Button>
      </div>
    </form>
  )
}
