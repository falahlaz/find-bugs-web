import { GitBranch, LoaderCircle, Lock, LockOpen, MessageSquare, Send } from 'lucide-react'
import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { Schemas } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useAskTrace, useRetrace, useSetTraceOpen, useTrace, useTraceRefs, type TraceSession } from './queries'
import { CodeTraceView, FieldLabel } from './result-view'

type CodeTrace = Schemas['CodeTrace']
type TraceMessage = Schemas['TraceMessage']

/** One traced version: the first trace, then each re-trace. */
type TracedVersion = { key: string; label: string; trace: CodeTrace }

function versionLabel(t: CodeTrace) {
  const where = t.refSource === 'manual' ? (t.env ? `deploy ${t.env}` : t.ref) : t.env || t.ref || 'main'
  return `${where} @ ${t.commit?.slice(0, 8) ?? '?'}`
}

/**
 * The code trace card body: the trace of each version looked at, a picker
 * to trace another version, and the chat about it.
 */
export function TraceSection({ jobId, initial }: { jobId: number; initial: CodeTrace }) {
  const hasSession = initial.status === 'found' || initial.status === 'not_found'
  const session = useTrace(jobId, hasSession)
  const s = session.data ?? null

  const versions: TracedVersion[] = [{ key: 'initial', label: `Awal · ${versionLabel(initial)}`, trace: initial }]
  for (const m of s?.messages ?? []) {
    if (m.role === 'assistant' && m.kind === 'retrace' && m.codeTrace) {
      versions.push({ key: String(m.id), label: versionLabel(m.codeTrace), trace: m.codeTrace })
    }
  }
  const [selected, setSelected] = useState<string | null>(null)
  // Jump to a re-trace when it arrives.
  const count = versions.length
  const seen = useRef(count)
  useEffect(() => {
    if (count > seen.current) setSelected(null)
    seen.current = count
  }, [count])
  const current = versions.find((v) => v.key === selected) ?? versions[versions.length - 1]

  return (
    <div className="grid gap-5">
      {versions.length > 1 && (
        <div className="flex flex-wrap gap-1.5" role="tablist" aria-label="Versi kode">
          {versions.map((v) => (
            <button
              key={v.key}
              role="tab"
              aria-selected={v.key === current.key}
              onClick={() => setSelected(v.key)}
              className={cn(
                'rounded-md border px-2.5 py-1 font-mono text-xs transition-colors',
                v.key === current.key ? 'border-primary bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary',
              )}
            >
              {v.label}
            </button>
          ))}
        </div>
      )}
      <CodeTraceView trace={current.trace} />
      {s && (
        <>
          <RetracePicker jobId={jobId} session={s} />
          <TraceChat jobId={jobId} session={s} onShowTrace={setSelected} />
        </>
      )}
    </div>
  )
}

function RetracePicker({ jobId, session }: { jobId: number; session: TraceSession }) {
  const projects = [...new Set(session.repos.map((r) => r.project))]
  const [open, setOpen] = useState(false)
  const [project, setProject] = useState(projects[0] ?? '')
  const [search, setSearch] = useState('')
  const q = useDebounced(search.trim(), 300)
  const refs = useTraceRefs(jobId, project, q, open)
  const retrace = useRetrace(jobId)
  const disabled = session.busy || session.state !== 'open' || retrace.isPending

  const run = (body: { env?: string; ref?: string }) =>
    retrace.mutate({ project, ...body }, { onSuccess: () => setOpen(false) })

  if (!open) {
    return (
      <div>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)} disabled={session.state !== 'open'}>
          <GitBranch aria-hidden />
          Cek versi lain
        </Button>
      </div>
    )
  }
  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <FieldLabel>Trace ulang di versi lain</FieldLabel>
        <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>
          Batal
        </Button>
      </div>
      {projects.length > 1 && (
        <Select value={project} onChange={(e) => setProject(e.target.value)} aria-label="Project">
          {projects.map((p) => (
            <option key={p}>{p}</option>
          ))}
        </Select>
      )}
      <div className="grid gap-1.5">
        <div className="text-xs text-muted-foreground">Yang sedang ter-deploy</div>
        {refs.isLoading && <p className="text-sm text-muted-foreground">Memuat deployment…</p>}
        <div className="grid gap-1.5 sm:grid-cols-2">
          {refs.data?.deployments.map((d) => (
            <button
              key={d.env}
              disabled={disabled || !d.commit}
              onClick={() => run({ env: d.env })}
              className="grid gap-0.5 rounded-md border px-3 py-2 text-left text-sm transition-colors hover:bg-secondary disabled:pointer-events-none disabled:opacity-50"
            >
              <span className="font-medium">{d.env}</span>
              <span className="truncate font-mono text-xs text-muted-foreground">
                {d.commit ? `${d.ref} @ ${d.commit.slice(0, 8)}` : d.error}
              </span>
              {d.finishedAt && <span className="text-xs text-muted-foreground">{formatDateTime(d.finishedAt)}</span>}
            </button>
          ))}
        </div>
      </div>
      <div className="grid gap-1.5">
        <div className="text-xs text-muted-foreground">Branch, tag atau commit</div>
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (search.trim()) run({ ref: search.trim() })
          }}
        >
          <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="mis. hotfix/MTA-2255 atau 9.4.1" className="font-mono" />
          <Button type="submit" size="sm" className="h-9" disabled={disabled || !search.trim()}>
            Trace
          </Button>
        </form>
        {(refs.data?.branches.length ?? 0) > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {refs.data?.branches.map((b) => (
              <button
                key={b.name}
                disabled={disabled}
                onClick={() => run({ ref: b.name })}
                className="rounded-md border px-2 py-1 font-mono text-xs hover:bg-secondary disabled:opacity-50"
              >
                {b.name}
              </button>
            ))}
          </div>
        )}
      </div>
      {retrace.error && <Alert tone="danger">{retrace.error.message}</Alert>}
    </div>
  )
}

function TraceChat({ jobId, session, onShowTrace }: { jobId: number; session: TraceSession; onShowTrace: (key: string) => void }) {
  const [text, setText] = useState('')
  const ask = useAskTrace(jobId)
  const setOpen = useSetTraceOpen(jobId)
  const isOpen = session.state === 'open'
  const canSend = isOpen && !session.busy && !ask.isPending && text.trim() !== ''
  const send = () => {
    if (!canSend) return
    ask.mutate(text.trim(), { onSuccess: () => setText('') })
  }

  return (
    <div className="grid gap-3 border-t pt-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-sm font-medium">
          <MessageSquare className="size-4 text-primary" aria-hidden />
          Tanya soal trace ini
          {!isOpen && <Badge tone="neutral">Sesi ditutup</Badge>}
        </div>
        <Button variant="ghost" size="sm" disabled={session.busy || setOpen.isPending} onClick={() => setOpen.mutate(!isOpen)}>
          {isOpen ? <Lock aria-hidden /> : <LockOpen aria-hidden />}
          {isOpen ? 'Tutup sesi' : 'Buka sesi'}
        </Button>
      </div>
      {session.messages.length > 0 && (
        <ol className="grid gap-3">
          {session.messages.map((m) => (
            <li key={m.id}>
              <ChatMessage m={m} onShowTrace={onShowTrace} />
            </li>
          ))}
        </ol>
      )}
      {!isOpen && (
        <p className="text-xs text-muted-foreground">
          {session.state === 'purged'
            ? 'Sesi lama sudah dibersihkan dari server. Membuka lagi memulai sesi baru dengan ringkasan percakapan di atas.'
            : session.idleClosed
              ? `Ditutup otomatis setelah ${session.idleMinutes} menit tanpa pesan. Buka lagi untuk melanjutkan; konteksnya masih utuh.`
              : 'Buka lagi untuk melanjutkan; konteksnya masih utuh.'}
        </p>
      )}
      {isOpen && (
        <form
          className="grid gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            send()
          }}
        >
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                e.preventDefault()
                send()
              }
            }}
            placeholder="Mis. kenapa nilainya null di sini? Apa fix yang aman?"
            maxLength={4000}
            rows={3}
          />
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted-foreground">Ctrl+Enter untuk kirim. Claude hanya bisa membaca kode dan log, tidak mengubah apa pun.</span>
            <Button type="submit" size="sm" disabled={!canSend}>
              <Send aria-hidden />
              Kirim
            </Button>
          </div>
          {ask.error && <Alert tone="danger">{ask.error.message}</Alert>}
        </form>
      )}
    </div>
  )
}

function ChatMessage({ m, onShowTrace }: { m: TraceMessage; onShowTrace: (key: string) => void }) {
  if (m.role === 'user') {
    return (
      <div className="ml-auto grid max-w-[85%] gap-1 rounded-lg bg-primary/10 px-3 py-2 text-sm">
        <div className="text-xs text-muted-foreground">
          {m.username || 'engineer'} · {formatDateTime(m.createdAt)}
        </div>
        <div className="break-words whitespace-pre-wrap">{m.content}</div>
      </div>
    )
  }
  if (m.status === 'queued' || m.status === 'running') {
    const lines = m.progress.slice(-5)
    return (
      <div className="grid max-w-[85%] gap-1.5 rounded-lg border px-3 py-2 text-sm">
        <div className="flex items-center gap-2 text-muted-foreground">
          <LoaderCircle className="size-4 animate-spin" aria-hidden />
          {m.status === 'queued' ? 'Menunggu giliran…' : 'Claude sedang menelusuri kode…'}
        </div>
        {lines.length > 0 && (
          <ul className="grid gap-0.5 font-mono text-xs text-muted-foreground">
            {lines.map((l, i) => (
              <li key={i} className="truncate">
                {l}
              </li>
            ))}
          </ul>
        )}
      </div>
    )
  }
  if (m.status === 'failed') {
    return <Alert tone="warning">{m.error || 'Gagal menjawab.'}</Alert>
  }
  if (m.kind === 'retrace' && m.codeTrace) {
    const t = m.codeTrace
    return (
      <div className="grid max-w-[85%] gap-1 rounded-lg border px-3 py-2 text-sm">
        <div>
          Trace ulang di <span className="font-mono">{versionLabel(t)}</span>:{' '}
          {t.status === 'found' ? <span className="font-mono">{`${t.file}:${t.line}`}</span> : 'lokasi tidak ditemukan'}
        </div>
        <button className="justify-self-start text-xs text-primary hover:underline" onClick={() => onShowTrace(String(m.id))}>
          Lihat hasilnya
        </button>
      </div>
    )
  }
  return (
    <div className="grid max-w-[92%] gap-1 rounded-lg border px-3 py-2 text-sm">
      <Markdown text={m.content} />
      {m.model && <div className="text-[11px] text-muted-foreground">{m.model}</div>}
    </div>
  )
}

/**
 * A small Markdown subset (code fences, inline code, bold) rendered as
 * React elements, never as HTML: answers quote untrusted logs and code.
 */
function Markdown({ text }: { text: string }) {
  const parts = text.split(/^```[^\n]*\n?/m)
  return (
    <div className="grid gap-2">
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <pre key={i} className="overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-[12.5px] leading-relaxed">
            {part.replace(/\n$/, '')}
          </pre>
        ) : (
          part
            .split(/\n{2,}/)
            .filter((p) => p.trim() !== '')
            .map((p, j) => (
              <p key={`${i}-${j}`} className="break-words whitespace-pre-wrap">
                {inline(p.trim())}
              </p>
            ))
        ),
      )}
    </div>
  )
}

function inline(s: string): ReactNode[] {
  return s.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*)/).map((t, i) => {
    if (t.startsWith('`') && t.endsWith('`') && t.length > 2) {
      return (
        <code key={i} className="rounded bg-muted px-1 py-0.5 font-mono text-[12.5px]">
          {t.slice(1, -1)}
        </code>
      )
    }
    if (t.startsWith('**') && t.endsWith('**') && t.length > 4) return <strong key={i}>{t.slice(2, -2)}</strong>
    return <Fragment key={i}>{t}</Fragment>
  })
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}
