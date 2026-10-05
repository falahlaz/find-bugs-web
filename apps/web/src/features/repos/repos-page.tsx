import { ExternalLink, GitBranch, Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { PageHeader } from '@/components/ui/page-header'
import { useTimezone } from '@/features/findbugs/queries'
import { formatDateTime, formatShort } from '@/lib/format'
import { useCloneRepo, useRepos, useStartSession, useStopSession, type Repo, type RepoClone } from './queries'

function CloneForm({ defaultGroup }: { defaultGroup: string }) {
  const clone = useCloneRepo()
  const [project, setProject] = useState('')

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    clone.mutate(project.trim(), { onSuccess: () => setProject('') })
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
      <div className="grid gap-1.5">
        <Label htmlFor="clone-project">Project GitLab</Label>
        <Input
          id="clone-project"
          required
          maxLength={200}
          autoComplete="off"
          spellCheck={false}
          placeholder="service-xyz atau group/project"
          value={project}
          onChange={(e) => setProject(e.target.value)}
        />
      </div>
      <Button type="submit" disabled={clone.isPending || !project.trim()}>
        Clone
      </Button>
      <p className="text-xs text-muted-foreground sm:col-span-2">
        Tanpa group berarti <span className="font-mono">{defaultGroup}/…</span>. Full clone di branch default, berjalan di belakang; token GitLab tidak disimpan di repo.
      </p>
      {clone.error && (
        <Alert tone="danger" className="sm:col-span-2">
          {clone.error.message}
        </Alert>
      )}
    </form>
  )
}

function CloneStatus({ c }: { c: RepoClone }) {
  const tz = useTimezone()
  if (c.state === 'cloning') {
    return (
      <Alert tone="info">
        Meng-clone <span className="font-mono">{c.project}</span> (oleh {c.by}, mulai {formatDateTime(c.startedAt, tz)})…
      </Alert>
    )
  }
  return (
    <Alert tone="danger">
      Clone <span className="font-mono">{c.project}</span> gagal: {c.error}
    </Alert>
  )
}

function RepoItem({ repo, canSession }: { repo: Repo; canSession: boolean }) {
  const tz = useTimezone()
  const start = useStartSession()
  const stop = useStopSession()
  const busy = start.isPending || stop.isPending
  const s = repo.session
  const error = start.error ?? stop.error

  return (
    <li className="grid gap-2 border-b py-3 last:border-0 sm:grid-cols-[1fr_auto] sm:items-center">
      <div className="grid min-w-0 gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm font-semibold break-all">{repo.project}</span>
          {s && (
            <Badge tone="success" dot pulse={!s.url}>
              {s.url ? 'Sesi jalan' : 'Menyambungkan'}
            </Badge>
          )}
          {repo.shallow && <Badge tone="neutral">shallow</Badge>}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <GitBranch className="size-3.5" aria-hidden />
            {repo.branch || (repo.commit ? 'detached' : 'kosong')}
          </span>
          {repo.commit && (
            <span className="min-w-0 truncate">
              <span className="font-mono">{repo.commit.slice(0, 8)}</span> {repo.subject}
            </span>
          )}
          {repo.committedAt && <span title={formatDateTime(repo.committedAt, tz)}>{formatShort(repo.committedAt, tz)}</span>}
        </div>
        {s && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>
              tmux <span className="font-mono">{s.name}</span> sejak {formatDateTime(s.since, tz)}
            </span>
            {s.url && (
              <a href={s.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-medium text-primary hover:underline">
                Buka di claude.ai
                <ExternalLink className="size-3.5" aria-hidden />
              </a>
            )}
          </div>
        )}
        {error && <p className="text-sm text-destructive break-words">{error.message}</p>}
      </div>
      {canSession && (
        <div className="flex gap-2 sm:justify-end">
          {s ? (
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => {
                if (window.confirm(`Stop sesi ${s.name}? Semua sesi Claude yang dibuka dari situ ikut berhenti.`)) stop.mutate(repo.project)
              }}
            >
              {stop.isPending && <Loader2 className="animate-spin" aria-hidden />}
              Stop
            </Button>
          ) : (
            <Button size="sm" disabled={busy || !repo.commit} onClick={() => start.mutate(repo.project)}>
              {start.isPending && <Loader2 className="animate-spin" aria-hidden />}
              {start.isPending ? 'Menyambungkan… ±30 dtk' : 'Mulai sesi'}
            </Button>
          )}
        </div>
      )}
    </li>
  )
}

export function ReposPage() {
  const repos = useRepos()
  const data = repos.data

  return (
    <div className="grid gap-4">
      <PageHeader
        title="Repo"
        description="Repo service di server. Mulai sesi Claude Remote Control (sama seperti /rc-session) lalu lanjutkan dari claude.ai/code atau aplikasi Claude."
      />
      {data?.canClone && (
        <Card>
          <CardHeader>
            <CardTitle>Clone repo baru</CardTitle>
          </CardHeader>
          <CardContent>
            <CloneForm defaultGroup={data.defaultGroup} />
          </CardContent>
        </Card>
      )}
      {data?.clones.map((c) => <CloneStatus key={c.project} c={c} />)}
      <Card>
        <CardHeader>
          <CardTitle>Semua repo</CardTitle>
        </CardHeader>
        <CardContent>
          {repos.error && <Alert tone="danger">{repos.error.message}</Alert>}
          {data && !data.canSession && (
            <Alert tone="warning" className="mb-2">
              Script rc-session tidak ada di server, jadi sesi tidak bisa dimulai dari sini.
            </Alert>
          )}
          {data && data.repos.length === 0 && <p className="text-sm text-muted-foreground">Belum ada repo.</p>}
          <ul>
            {data?.repos.map((r) => <RepoItem key={r.project} repo={r} canSession={data.canSession} />)}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}
