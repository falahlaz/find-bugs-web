import { useQuery } from '@tanstack/react-query'
import { api, ApiError, unwrap, type Schemas } from '@/lib/api'

export type Report = Schemas['ReportView']

export function useReports() {
  return useQuery({
    queryKey: ['reports'],
    queryFn: async () => unwrap(await api.GET('/api/reports')),
    refetchInterval: 60_000,
  })
}

/** URL of one report file; `download` makes the server send it as an attachment. */
export function reportFileURL(r: Pick<Report, 'repo' | 'slug'>, file: 'report.md' | 'report.html', download = false) {
  return `/api/reports/${encodeURIComponent(r.repo)}/${encodeURIComponent(r.slug)}/${file}${download ? '?download=1' : ''}`
}

export function vaultURL(repo?: string) {
  return `/api/reports/archive.zip${repo ? `?repo=${encodeURIComponent(repo)}` : ''}`
}

/** report.md is served raw (not JSON), so it's fetched directly. */
export function useReportMarkdown(r: Pick<Report, 'repo' | 'slug'>, enabled: boolean) {
  return useQuery({
    queryKey: ['report-md', r.repo, r.slug],
    enabled,
    queryFn: async () => {
      const res = await fetch(reportFileURL(r, 'report.md'), { credentials: 'same-origin' })
      if (!res.ok) {
        const e = (await res.json().catch(() => undefined)) as { error?: string; code?: string } | undefined
        throw new ApiError(res.status, e?.error ?? `HTTP ${res.status}`, e?.code)
      }
      return res.text()
    },
  })
}
