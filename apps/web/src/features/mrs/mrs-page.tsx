import { useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Check, ClipboardCopy, ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { Segmented } from '@/components/ui/segmented'
import { Markdown } from '@/features/findbugs/markdown'
import { useTimezone } from '@/features/findbugs/queries'
import { useCloneRepo, useStartSession } from '@/features/repos/queries'
import { formatDateTime, formatShort } from '@/lib/format'
import { cn } from '@/lib/utils'
import { sessionPrompt } from './prompt'
import { mrCommentsQuery, mrConflictsQuery, mrKey, useMergeMR, useMRComments, useMRConflicts, useMRs, type MR, type MRState } from './queries'

// GitLab statuses under which has_conflicts may be out of date.
const staleStatus = new Set(['checking', 'unchecked', 'preparing', 'approvals_syncing'])

type MergeStatus = { state: 'merging' } | { state: 'ok' } | { state: 'error'; message: string }

function approvalText(mr: MR) {
  const a = mr.approvals
  if (!a) return 'approval tidak terbaca'
  if (a.approved) return `approved oleh ${a.approvedBy.join(', ') || 'seseorang'}`
  if (a.required === 0) return 'tidak butuh approval'
  return `butuh ${a.left} approval lagi`
}

function CommentsList({ mr }: { mr: MR }) {
  const q = useMRComments(mr, true)
  if (q.isPending) return <p className="text-sm text-muted-foreground">Memuat komentar…</p>
  if (q.error) return <p className="text-sm text-destructive">{q.error.message}</p>
  if (q.data.comments.length === 0) return <p className="text-sm text-muted-foreground">Tidak ada komentar yang belum resolved.</p>
  return (
    <ol className="grid gap-2">
      {q.data.comments.map((c, i) => (
        <li key={i} className="rounded-md border bg-muted/40 p-3">
          <p className="mb-1 text-xs text-muted-foreground">
            <span className="font-mono">{c.path ? `${c.path}:${c.line ?? ''}` : 'Thread umum'}</span> · {c.author}
            {c.replies > 0 && ` · ${c.replies} balasan`}
          </p>
          <div className="line-clamp-6 text-sm">
            <Markdown text={c.body} />
          </div>
        </li>
      ))}
    </ol>
  )
}

function ConflictFiles({ mr, label }: { mr: MR; label: string }) {
  const [check, setCheck] = useState(false)
  const files = useMRConflicts(mr, check)
  return (
    <>
      <Button size="sm" variant="outline" disabled={files.isFetching} onClick={() => (check ? void files.refetch() : setCheck(true))}>
        {files.isFetching && <Loader2 className="animate-spin" aria-hidden />}
        {label}
      </Button>
      {(files.error || files.data) && (
        <div className="basis-full text-sm">
          {files.error && <p className="text-destructive">{files.error.message}</p>}
          {files.data &&
            (files.data.files.length === 0 ? (
              <p className="text-muted-foreground">Tidak ada konflik saat {mr.source} di-merge ke {mr.target} (dicek lokal).</p>
            ) : (
              <>
                <p className="text-destructive">{files.data.files.length} file konflik (dicek lokal):</p>
                <ul className="grid gap-0.5 font-mono text-xs">
                  {files.data.files.map((f) => (
                    <li key={f} className="break-all">
                      {f}
                    </li>
                  ))}
                </ul>
              </>
            ))}
        </div>
      )}
    </>
  )
}

function SessionButton({ mr, onChanged }: { mr: MR; onChanged: () => void }) {
  const start = useStartSession()
  const repo = mr.repo
  if (!repo) return null
  const session = repo.session
  if (session) {
    return session.url ? (
      <a href={session.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline">
        Buka sesi Claude
        <ExternalLink className="size-3.5" aria-hidden />
      </a>
    ) : (
      <Badge tone="success" dot pulse>
        Sesi menyambungkan
      </Badge>
    )
  }
  return (
    <>
      <Button size="sm" variant="outline" disabled={start.isPending || !repo.commit} onClick={() => start.mutate(mr.project, { onSuccess: onChanged })}>
        {start.isPending && <Loader2 className="animate-spin" aria-hidden />}
        {start.isPending ? 'Menyambungkan… ±30 dtk' : 'Mulai sesi RC'}
      </Button>
      {start.error && <p className="basis-full text-sm text-destructive">{start.error.message}</p>}
    </>
  )
}

function CloneButton({ mr, onChanged }: { mr: MR; onChanged: () => void }) {
  const clone = useCloneRepo()
  return (
    <>
      <span className="text-sm text-muted-foreground">
        Repo <span className="font-mono">{mr.project}</span> belum ada di server.
      </span>
      {clone.isSuccess ? (
        <span className="text-sm text-muted-foreground">
          Clone berjalan, pantau di{' '}
          <Link to="/repos" className="font-medium text-primary hover:underline">
            halaman Repo
          </Link>
          .
        </span>
      ) : (
        <Button size="sm" variant="outline" disabled={clone.isPending} onClick={() => clone.mutate(mr.project, { onSuccess: onChanged })}>
          {clone.isPending && <Loader2 className="animate-spin" aria-hidden />}
          Clone repo
        </Button>
      )}
      {clone.error && <p className="basis-full text-sm text-destructive">{clone.error.message}</p>}
    </>
  )
}

function SessionPrompt({ mr }: { mr: MR }) {
  const qc = useQueryClient()
  const [text, setText] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable (http or denied); the text stays selectable */
    }
  }

  async function prepare() {
    setBusy(true)
    try {
      const [comments, conflicts] = await Promise.allSettled([
        mr.notes > 0 ? qc.fetchQuery(mrCommentsQuery(mr)) : Promise.resolve({ comments: [] }),
        mr.hasConflicts ? qc.fetchQuery(mrConflictsQuery(mr)) : Promise.resolve({ files: [] }),
      ])
      const t = sessionPrompt(mr, {
        comments: comments.status === 'fulfilled' ? comments.value.comments : [],
        files: conflicts.status === 'fulfilled' ? conflicts.value.files : undefined,
        conflictError: conflicts.status === 'rejected' ? String((conflicts.reason as Error)?.message ?? conflicts.reason) : undefined,
      })
      setText(t)
      await copy(t)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Button size="sm" variant="outline" disabled={busy} onClick={() => void prepare()} title="Prompt berisi MR, branch, file konflik dan komentar, untuk ditempel di sesi Claude">
        {busy ? <Loader2 className="animate-spin" aria-hidden /> : copied ? <Check aria-hidden /> : <ClipboardCopy aria-hidden />}
        {copied ? 'Prompt tersalin' : text ? 'Salin ulang prompt' : 'Salin prompt sesi'}
      </Button>
      {text && (
        <div className="grid basis-full gap-1">
          <p className="text-xs text-muted-foreground">Tempel ke sesi Claude sebagai pesan pertama. Claude akan berhenti sebelum push.</p>
          <textarea
            readOnly
            value={text}
            rows={10}
            onFocus={(e) => e.currentTarget.select()}
            className="w-full resize-y rounded-md border bg-muted/40 p-2 font-mono text-xs"
          />
        </div>
      )}
    </>
  )
}

/**
 * Tools for an open MR. One with conflicts or comments gets an RC session in
 * its repo plus a ready prompt (or a clone button when the repo is missing);
 * GitLab-flagged conflicts also get the file list. A clean MR with a cloned
 * repo gets a local conflict check, since GitLab's has_conflicts can be stale.
 */
function WorkTools({ mr, onChanged }: { mr: MR; onChanged: () => void }) {
  const needsWork = mr.hasConflicts || mr.notes > 0
  if (!needsWork) {
    if (!mr.repo) return null
    return (
      <div className="flex flex-wrap items-center gap-2">
        <ConflictFiles mr={mr} label="Cek konflik lokal" />
      </div>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      {mr.repo ? (
        <>
          <SessionButton mr={mr} onChanged={onChanged} />
          <SessionPrompt mr={mr} />
          <ConflictFiles mr={mr} label={mr.hasConflicts ? 'Cek file konflik' : 'Cek konflik lokal'} />
        </>
      ) : (
        <CloneButton mr={mr} onChanged={onChanged} />
      )}
    </div>
  )
}

function MRRow({ mr, status, onMerge, onChanged }: { mr: MR; status?: MergeStatus; onMerge?: () => void; onChanged: () => void }) {
  const tz = useTimezone()
  const [showComments, setShowComments] = useState(false)
  const open = mr.state === 'opened'
  const merging = status?.state === 'merging'

  return (
    <li className="grid gap-2 border-b py-3 last:border-0">
      <div className="grid gap-2 sm:grid-cols-[1fr_auto] sm:items-start">
        <div className="grid min-w-0 gap-1">
          <a href={mr.webUrl} target="_blank" rel="noreferrer" className="font-medium break-words hover:underline">
            <span className="font-mono text-muted-foreground">!{mr.iid}</span> {mr.title}
          </a>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span className="font-mono break-all">{mr.project}</span>
            <span className="inline-flex items-center gap-1 font-mono break-all">
              {mr.source} <ArrowRight className="size-3" aria-hidden /> {mr.target}
            </span>
            {open && <span>{approvalText(mr)}</span>}
            <span title={formatDateTime(mr.updatedAt, tz)}>{formatShort(mr.updatedAt, tz)}</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {!open && <Badge tone={mr.state === 'merged' ? 'success' : 'neutral'}>{mr.state}</Badge>}
            {mr.draft && <Badge tone="neutral">draft</Badge>}
            {open && mr.hasConflicts && <Badge tone="danger">konflik</Badge>}
            {open && staleStatus.has(mr.mergeStatus ?? '') && (
              <Badge tone="neutral" title="GitLab belum menghitung ulang mergeability; status konflik bisa basi. Refresh sebentar lagi atau cek lokal.">
                GitLab masih mengecek
              </Badge>
            )}
            {open && mr.notes > 0 && (
              <button type="button" onClick={() => setShowComments((v) => !v)}>
                <Badge tone="warning" className="cursor-pointer">
                  {mr.notes} komentar {showComments ? '▴' : '▾'}
                </Badge>
              </button>
            )}
          </div>
        </div>
        {onMerge && (
          <div className="flex items-center gap-2 sm:justify-end">
            {status?.state === 'ok' && <Badge tone="success">merged</Badge>}
            {status?.state !== 'ok' && (
              <Button size="sm" disabled={merging} onClick={onMerge}>
                {merging && <Loader2 className="animate-spin" aria-hidden />}
                Merge
              </Button>
            )}
          </div>
        )}
      </div>
      {status?.state === 'error' && <p className="text-sm text-destructive break-words">{status.message}</p>}
      {showComments && <CommentsList mr={mr} />}
      {open && <WorkTools mr={mr} onChanged={onChanged} />}
    </li>
  )
}

function Bucket({ title, count, tone, actions, children }: { title: string; count: number; tone: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2">
        <CardTitle className="flex items-center gap-2">
          <span className={cn('size-2 rounded-full', tone)} aria-hidden />
          {title}
          <span className="text-muted-foreground tabular-nums">{count}</span>
        </CardTitle>
        {actions}
      </CardHeader>
      <CardContent>{count === 0 ? <p className="text-sm text-muted-foreground">Kosong.</p> : <ul>{children}</ul>}</CardContent>
    </Card>
  )
}

function Stat({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="rounded-lg border bg-card p-3">
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span className={cn('size-2 rounded-full', tone)} aria-hidden />
        {label}
      </p>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  )
}

export function MRsPage() {
  const [state, setState] = useState<MRState>('opened')
  const mrs = useMRs(state)
  const merge = useMergeMR()
  const qc = useQueryClient()
  const [statuses, setStatuses] = useState<Record<string, MergeStatus>>({})
  const [mergingAll, setMergingAll] = useState(false)
  const data = mrs.data
  const refresh = () => void qc.invalidateQueries({ queryKey: ['mrs'] })

  async function mergeOne(mr: MR) {
    setStatuses((s) => ({ ...s, [mrKey(mr)]: { state: 'merging' } }))
    try {
      await merge.mutateAsync(mr)
      setStatuses((s) => ({ ...s, [mrKey(mr)]: { state: 'ok' } }))
    } catch (e) {
      setStatuses((s) => ({ ...s, [mrKey(mr)]: { state: 'error', message: e instanceof Error ? e.message : String(e) } }))
    }
  }

  async function onMerge(mr: MR) {
    if (!window.confirm(`Merge !${mr.iid} ke ${mr.target} dan hapus branch ${mr.source}?\n\n${mr.title}`)) return
    await mergeOne(mr)
    refresh()
  }

  async function onMergeAll(ready: MR[]) {
    const todo = ready.filter((mr) => statuses[mrKey(mr)]?.state !== 'ok')
    if (!window.confirm(`Merge ${todo.length} MR yang Ready dan hapus source branch-nya?\n\n${todo.map((m) => `!${m.iid} ${m.title}`).join('\n')}`)) return
    setMergingAll(true)
    for (const mr of todo) await mergeOne(mr)
    setMergingAll(false)
    refresh()
  }

  const row = (mr: MR, withMerge = false) => (
    <MRRow key={mrKey(mr)} mr={mr} status={statuses[mrKey(mr)]} onMerge={withMerge ? () => void onMerge(mr) : undefined} onChanged={refresh} />
  )

  return (
    <div className="grid gap-4">
      <PageHeader
        title="MR Triage"
        description={
          data
            ? `MR GitLab milik pemilik token yang judulnya memuat "| ${data.author} |". Merge memakai token server dan masuk audit log.`
            : 'MR GitLab milik pemilik token server, dikelompokkan Ready / Tunggu approval / Konflik / Komentar / Draft.'
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Segmented<MRState>
              label="Status MR"
              value={state}
              onChange={setState}
              options={[
                { value: 'opened', label: 'Open' },
                { value: 'all', label: 'Semua status' },
              ]}
            />
            <Button variant="outline" size="sm" onClick={() => void mrs.refetch()} disabled={mrs.isFetching}>
              <RefreshCw className={cn(mrs.isFetching && 'animate-spin')} /> Refresh
            </Button>
          </div>
        }
      />
      {mrs.error && <Alert tone="danger">{mrs.error.message}</Alert>}
      {mrs.isPending && <p className="text-sm text-muted-foreground">Mengambil MR dari GitLab…</p>}
      {data && (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
            <Stat label="Ready" value={data.ready.length} tone="bg-ok" />
            <Stat label="Tunggu approval" value={data.waiting.length} tone="bg-info" />
            <Stat label="Konflik" value={data.conflict.length} tone="bg-bad" />
            <Stat label="Komentar" value={data.comments.length} tone="bg-warn" />
            <Stat label="Draft" value={data.drafts.length} tone="bg-neu" />
          </div>
          <Bucket
            title="Siap di-merge"
            count={data.ready.length}
            tone="bg-ok"
            actions={
              data.ready.length > 1 && (
                <Button size="sm" disabled={mergingAll || merge.isPending} onClick={() => void onMergeAll(data.ready)}>
                  {mergingAll && <Loader2 className="animate-spin" aria-hidden />}
                  Merge semua Ready
                </Button>
              )
            }
          >
            {data.ready.map((mr) => row(mr, true))}
          </Bucket>
          <Bucket title="Menunggu approval" count={data.waiting.length} tone="bg-info">
            {data.waiting.map((mr) => row(mr))}
          </Bucket>
          <Bucket title="Ada konflik" count={data.conflict.length} tone="bg-bad">
            {data.conflict.map((mr) => row(mr))}
          </Bucket>
          <Bucket title="Ada komentar" count={data.comments.length} tone="bg-warn">
            {data.comments.map((mr) => row(mr))}
          </Bucket>
          <Bucket title="Draft" count={data.drafts.length} tone="bg-neu">
            {data.drafts.map((mr) => row(mr))}
          </Bucket>
          {state === 'all' && (
            <Bucket title="Merged & closed" count={data.others.length} tone="bg-neu">
              {data.others.map((mr) => row(mr))}
            </Bucket>
          )}
        </>
      )}
    </div>
  )
}
