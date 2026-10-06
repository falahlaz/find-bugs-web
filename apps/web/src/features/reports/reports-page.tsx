import { Download, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button-variants'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { Select } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { useReports, vaultURL, type Report } from './queries'
import { ReportBadges } from './report-badges'

function matches(r: Report, q: string) {
  if (!q) return true
  const hay = [r.title, r.summary, r.repo, r.slug, ...r.tags, ...r.endpoints].join('\n').toLowerCase()
  return q
    .toLowerCase()
    .split(/\s+/)
    .every((w) => hay.includes(w))
}

function ReportItem({ r }: { r: Report }) {
  return (
    <li className="border-b last:border-0">
      <Link to={`/laporan/${encodeURIComponent(r.repo)}/${encodeURIComponent(r.slug)}`} className="grid gap-1.5 py-3 hover:bg-secondary/50 sm:px-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-semibold">{r.title}</span>
          <ReportBadges status={r.status} severity={r.severity} />
        </div>
        {r.summary && <p className="text-sm text-muted-foreground">{r.summary}</p>}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {r.date && <span>{r.date}</span>}
          <span className="font-mono">{r.repo}</span>
          {r.endpoints.length > 0 && <span className="font-mono break-all">{r.endpoints.join(', ')}</span>}
        </div>
        {r.tags.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {r.tags.map((t) => (
              <Badge key={t} tone="outline">
                {t}
              </Badge>
            ))}
          </div>
        )}
      </Link>
    </li>
  )
}

export function ReportsPage() {
  const reports = useReports()
  const [q, setQ] = useState('')
  const [repo, setRepo] = useState('')
  const data = reports.data
  const repos = useMemo(() => [...new Set(data?.reports.map((r) => r.repo))].sort(), [data])
  const shown = data?.reports.filter((r) => (!repo || r.repo === repo) && matches(r, q.trim())) ?? []

  return (
    <div className="grid gap-4">
      <PageHeader
        title="Laporan"
        description="Hasil trace dari skill trace-report: laporan stakeholder (HTML) dan laporan teknis (Markdown)."
        actions={
          data?.canReadMd &&
          data.reports.length > 0 && (
            <a
              href={vaultURL(repo || undefined)}
              download
              className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
              title="Extract lalu buka foldernya sebagai vault di Obsidian"
            >
              <Download aria-hidden />
              Download vault{repo ? ` ${repo}` : ''} (.zip)
            </a>
          )
        }
      />
      <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input className="pl-9" placeholder="Cari judul, tag, endpoint…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Cari laporan" />
        </div>
        {repos.length > 1 && (
          <Select value={repo} onChange={(e) => setRepo(e.target.value)} aria-label="Repo" className="sm:w-56">
            <option value="">Semua repo</option>
            {repos.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </Select>
        )}
      </div>
      <Card>
        <CardContent>
          {reports.error && <Alert tone="danger">{reports.error.message}</Alert>}
          {data && shown.length === 0 && (
            <p className="py-3 text-sm text-muted-foreground">{data.reports.length === 0 ? 'Belum ada laporan.' : 'Tidak ada laporan yang cocok.'}</p>
          )}
          <ul>
            {shown.map((r) => (
              <ReportItem key={`${r.repo}/${r.slug}`} r={r} />
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}
