import { ArrowLeft, Download, ExternalLink } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useParams } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { buttonVariants } from '@/components/ui/button-variants'
import { Card, CardContent } from '@/components/ui/card'
import { LinkButton } from '@/components/ui/link-button'
import { PageHeader } from '@/components/ui/page-header'
import { cn } from '@/lib/utils'
import { reportFileURL, useReportMarkdown, useReports, type Report } from './queries'
import { ReportBadges } from './report-badges'
import { ReportMarkdown } from './report-markdown'

type Tab = 'html' | 'md'

function FileLink({ href, children, download }: { href: string; children: ReactNode; download?: boolean }) {
  return (
    <a
      href={href}
      download={download || undefined}
      target={download ? undefined : '_blank'}
      rel="noreferrer"
      className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
    >
      {download ? <Download aria-hidden /> : <ExternalLink aria-hidden />}
      {children}
    </a>
  )
}

function TechnicalReport({ report }: { report: Report }) {
  const md = useReportMarkdown(report, true)
  if (md.error) return <Alert tone="danger">{md.error.message}</Alert>
  if (!md.data) return <p className="text-sm text-muted-foreground">Memuat…</p>
  return <ReportMarkdown text={md.data} repo={report.repo} slug={report.slug} />
}

export function ReportPage() {
  const { repo = '', slug = '' } = useParams()
  const reports = useReports()
  const report = reports.data?.reports.find((r) => r.repo === repo && r.slug === slug)
  const [picked, setPicked] = useState<Tab | null>(null)

  const back = (
    <LinkButton to="/laporan" variant="ghost" size="sm" className="w-fit">
      <ArrowLeft aria-hidden />
      Semua laporan
    </LinkButton>
  )
  if (reports.error) return <Alert tone="danger">{reports.error.message}</Alert>
  if (!reports.data) return <p className="text-sm text-muted-foreground">Memuat…</p>
  if (!report)
    return (
      <div className="grid gap-3">
        {back}
        <Alert tone="warning">Laporan tidak ditemukan.</Alert>
      </div>
    )

  const tabs: { key: Tab; label: string }[] = []
  if (report.hasHtml) tabs.push({ key: 'html', label: 'Stakeholder' })
  if (report.hasMd) tabs.push({ key: 'md', label: 'Teknis' })
  const tab = tabs.find((t) => t.key === picked)?.key ?? tabs[0]?.key

  return (
    <div className="grid gap-4">
      {back}
      <PageHeader
        title={report.title}
        description={report.summary}
        actions={
          <>
            {report.hasHtml && (
              <>
                <FileLink href={reportFileURL(report, 'report.html')}>Buka HTML</FileLink>
                <FileLink href={reportFileURL(report, 'report.html', true)} download>
                  .html
                </FileLink>
              </>
            )}
            {report.hasMd && (
              <FileLink href={reportFileURL(report, 'report.md', true)} download>
                .md
              </FileLink>
            )}
          </>
        }
      />
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
        <ReportBadges status={report.status} severity={report.severity} />
        {report.date && <span>{report.date}</span>}
        <span className="font-mono">{report.repo}</span>
        {report.endpoints.length > 0 && <span className="font-mono break-all">{report.endpoints.join(', ')}</span>}
      </div>
      {tabs.length > 1 && (
        <div className="flex flex-wrap gap-1.5" role="tablist" aria-label="Versi laporan">
          {tabs.map((t) => (
            <button
              key={t.key}
              role="tab"
              aria-selected={t.key === tab}
              onClick={() => setPicked(t.key)}
              className={cn(
                'rounded-md border px-3 py-1 text-sm transition-colors',
                t.key === tab ? 'border-primary bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary',
              )}
            >
              {t.label}
            </button>
          ))}
        </div>
      )}
      {tab === 'html' && (
        // Served with a CSP sandbox too: the report's script can't reach the app or its cookies.
        <iframe
          title={report.title}
          src={reportFileURL(report, 'report.html')}
          sandbox="allow-scripts allow-popups allow-modals"
          className="h-[80vh] w-full rounded-lg border bg-white"
        />
      )}
      {tab === 'md' && (
        <Card>
          <CardContent>
            <TechnicalReport report={report} />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
