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

/** Client-side hint only; the server does the real parsing. */
function detect(input: string): { tone: 'ok' | 'warn' | 'idle'; text: string; id?: string } {
  const v = input.trim()
  if (!v) return { tone: 'idle', text: 'Paste transaction ID atau curl lengkap.' }
  if (/^curl\s/i.test(v)) {
    const m = v.match(/X-Transaction-ID:\s*([^\s"'\\]+)/i)
    return m ? { tone: 'ok', text: 'curl terdeteksi, ID dari header X-Transaction-ID', id: m[1] } : { tone: 'warn', text: 'curl terdeteksi, tapi header X-Transaction-ID tidak terlihat.' }
  }
  if (/\s/.test(v)) return { tone: 'warn', text: 'Ada spasi. Paste satu transaction ID atau satu perintah curl.' }
  return { tone: 'ok', text: 'Transaction ID', id: v }
}

function Segmented<T extends string>({ value, options, onChange, label, hero }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string; hero?: boolean }) {
  return (
    <div role="radiogroup" aria-label={label} className={cn('inline-flex rounded-lg border p-0.5', hero ? 'border-white/20 bg-white/10' : 'bg-muted')}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-md px-3 py-1 text-xs font-medium transition-colors',
            hero ? 'text-white/80' : 'text-muted-foreground',
            value === o.value && (hero ? 'bg-white text-hero' : 'bg-card text-foreground shadow-xs'),
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/**
 * The investigation composer shared by every design: input with live
 * detection, environment and time range, duplicate handling, Ctrl+Enter.
 */
export function SubmitForm({
  idPrefix = 'submit',
  variant = 'card',
  onSubmitted,
  className,
}: {
  idPrefix?: string
  variant?: 'card' | 'plain' | 'hero'
  onSubmitted?: () => void
  className?: string
}) {
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
  const hint = detect(input)
  const hero = variant === 'hero'

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
    onSubmitted?.()
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
    <form
      onSubmit={onSubmit}
      className={cn(
        'grid',
        variant === 'card' && 'rounded-xl border bg-card shadow-[0_1px_2px_rgb(0_0_0/0.04)] transition-shadow focus-within:border-ring focus-within:shadow-[0_0_0_4px_var(--secondary)]',
        variant !== 'card' && 'gap-3',
        className,
      )}
    >
      <label htmlFor={`${idPrefix}-input`} className={cn('text-xs font-medium', hero ? 'text-white/75' : 'text-muted-foreground', variant === 'card' && 'sr-only')}>
        Transaction ID atau curl
      </label>
      <textarea
        id={`${idPrefix}-input`}
        rows={variant === 'plain' ? 6 : 5}
        spellCheck={false}
        required
        value={input}
        onChange={(e) => {
          setInput(e.target.value)
          setDuplicate(null)
        }}
        onKeyDown={onKeyDown}
        placeholder={'TRX-8F3A21C0-7B\natau curl -H "X-Transaction-ID: …" https://…'}
        className={cn(
          'w-full resize-y font-mono text-[13px] leading-relaxed outline-none',
          variant === 'card' && 'min-h-32 rounded-t-xl bg-transparent px-4 pt-4 pb-2',
          variant === 'plain' && 'rounded-lg border border-input bg-card px-3 py-2.5 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40',
          hero && 'rounded-xl border border-white/20 bg-white/10 px-3 py-2.5 text-white placeholder:text-white/45 focus:border-white/60',
        )}
      />
      <p
        aria-live="polite"
        className={cn('flex min-h-5 items-center gap-1.5 text-xs', variant === 'card' && 'px-4 pb-3', hero ? 'text-white/75' : 'text-muted-foreground')}
      >
        {hint.tone === 'ok' && <Check className={cn('size-3.5', hero ? 'text-emerald-200' : 'text-ok')} />}
        {hint.tone === 'warn' && <CircleAlert className={cn('size-3.5', hero ? 'text-amber-200' : 'text-warn')} />}
        <span>{hint.text}</span>
        {hint.id && <b className={cn('truncate font-mono font-medium', hero ? 'text-white' : 'text-foreground')}>{hint.id}</b>}
      </p>

      {(submit.error || duplicate) && (
        <div className={cn('grid gap-2', variant === 'card' && 'px-3 pb-3')}>
          {submit.error && <Alert tone="danger">{submit.error.message}</Alert>}
          {duplicate && (
            <Alert tone="info">
              <div className="grid gap-2">
                <span>
                  <b className="font-mono">{duplicate.transactionId}</b> sudah diinvestigasi pada {formatDateTime(duplicate.queuedAt, tz)} dengan parameter yang sama.
                </span>
                <span className="flex flex-wrap gap-2">
                  <LinkButton size="sm" to={`/jobs/${duplicate.id}`} onClick={onSubmitted}>
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

      <div className={cn('flex flex-wrap items-center gap-2', variant === 'card' && 'border-t px-3 py-2.5')}>
        {envList.length <= 4 ? (
          <Segmented label="Environment" value={env} hero={hero} options={envList.map((e) => ({ value: e, label: e }))} onChange={setEnvironment} />
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
          hero={hero}
          options={[
            { value: '24h', label: '24 jam' },
            { value: '48h', label: '48 jam' },
          ]}
          onChange={setTimeRange}
        />
        <Button type="submit" size="sm" disabled={submit.isPending || !env} className={cn('ml-auto', hero && 'bg-white text-hero hover:bg-white/90')}>
          {submit.isPending ? 'Mengirim…' : 'Investigasi'}
          <kbd className={cn('hidden rounded px-1 font-mono text-[10px] sm:inline-flex sm:items-center sm:gap-0.5', hero ? 'bg-hero/10' : 'bg-white/15')}>
            Ctrl <CornerDownLeft className="!size-2.5" />
          </kbd>
        </Button>
      </div>
    </form>
  )
}
