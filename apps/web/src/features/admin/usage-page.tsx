import { useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import antigravityLogo from '@/assets/antigravity.svg'
import claudeLogo from '@/assets/claude.svg'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { useTimezone } from '@/features/findbugs/queries'
import { api, ApiError, unwrap } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'

function fetchUsage(refresh: boolean) {
  return api.GET('/api/usage', { params: { query: refresh ? { refresh: '1' } : {} } }).then(unwrap)
}

function fetchAgyUsage(refresh: boolean) {
  return api.GET('/api/usage/agy', { params: { query: refresh ? { refresh: '1' } : {} } }).then(unwrap)
}

/** "2 jam 13 mnt lagi" until iso. */
function untilLabel(iso: string) {
  const m = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60_000))
  if (m < 60) return `${m} mnt lagi`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} jam ${m % 60} mnt lagi`
  return `${Math.floor(h / 24)} hari ${h % 24} jam lagi`
}

type UsageWindow = { percent: number; resetsAt: string }

function UsageBar({ title, window, tz, note }: { title: string; window: UsageWindow | null; tz: string; note?: string }) {
  if (!window) return null
  const pct = Math.min(100, Math.round(window.percent))
  return (
    <Card>
      <CardHeader className="flex-row items-baseline justify-between">
        <CardTitle>{title}</CardTitle>
        <span className="text-2xl font-semibold tabular-nums">{pct}%</span>
      </CardHeader>
      <CardContent className="grid gap-2">
        <div className="h-2.5 overflow-hidden rounded-full bg-secondary">
          <div className={cn('h-full rounded-full', pct >= 90 ? 'bg-bad' : pct >= 70 ? 'bg-warn' : 'bg-primary')} style={{ width: `${pct}%` }} />
        </div>
        <p className="text-sm text-muted-foreground">
          Reset {untilLabel(window.resetsAt)} · {formatDateTime(window.resetsAt, tz)}
        </p>
        {note && <p className="text-xs text-muted-foreground">{note}</p>}
      </CardContent>
    </Card>
  )
}

/** Names the models of a short pool; a long one only gets a count. */
function modelsNote(models: string[]) {
  return models.length <= 4 ? models.join(' · ') : `${models.length} model`
}

function Section({ logo, title, checkedAt, tz, children }: { logo: string; title: string; checkedAt?: string; tz: string; children: ReactNode }) {
  return (
    <section className="grid gap-3">
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <img src={logo} alt="" className="size-4" /> {title}
      </h2>
      {children}
      {checkedAt && <p className="text-xs text-muted-foreground">Terakhir dicek {formatDateTime(checkedAt, tz)}</p>}
    </section>
  )
}

export function UsagePage() {
  const tz = useTimezone()
  const qc = useQueryClient()
  const usage = useQuery({ queryKey: ['usage'], queryFn: () => fetchUsage(false), refetchInterval: 5 * 60_000 })
  const refresh = useQuery({ queryKey: ['usage', 'refresh'], queryFn: () => fetchUsage(true), enabled: false })
  const agy = useQuery({ queryKey: ['usage-agy'], queryFn: () => fetchAgyUsage(false), refetchInterval: 5 * 60_000, retry: false })
  const agyRefresh = useQuery({ queryKey: ['usage-agy', 'refresh'], queryFn: () => fetchAgyUsage(true), enabled: false, retry: false })
  const onRefresh = async () => {
    const [r, ra] = await Promise.all([refresh.refetch(), agyRefresh.refetch()])
    if (r.data) qc.setQueryData(['usage'], r.data)
    if (ra.data) qc.setQueryData(['usage-agy'], ra.data)
  }
  const data = usage.data
  const err = refresh.error ?? usage.error
  const agyErr = agyRefresh.error ?? agy.error
  // 404 means agy is not installed on the server: hide the section.
  const agyHidden = agyErr instanceof ApiError && agyErr.status === 404
  const busy = refresh.isFetching || usage.isFetching || agyRefresh.isFetching || agy.isFetching
  return (
    <div className="grid gap-6">
      <PageHeader
        title="Pemakaian AI"
        description="Limit langganan Claude dan Antigravity (agy), sama seperti di claude.ai dan agy. Dicek ulang tiap 5 menit."
        actions={
          <Button variant="outline" size="sm" onClick={onRefresh} disabled={busy}>
            <RefreshCw className={cn((refresh.isFetching || agyRefresh.isFetching) && 'animate-spin')} /> Cek sekarang
          </Button>
        }
      />
      <Section logo={claudeLogo} title="Claude" checkedAt={data?.checkedAt} tz={tz}>
        {err && <Alert tone="danger">{err.message}</Alert>}
        {data?.status === 'rejected' && <Alert tone="danger">Limit sudah habis; analyzer akan gagal sampai reset.</Alert>}
        {usage.isPending && <p className="text-sm text-muted-foreground">Mengecek…</p>}
        {data && (
          <div className="grid gap-4 md:grid-cols-2">
            <UsageBar title="Sesi 5 jam" window={data.fiveHour} tz={tz} />
            <UsageBar title="Mingguan" window={data.sevenDay} tz={tz} />
          </div>
        )}
      </Section>
      {!agyHidden && (
        <Section logo={antigravityLogo} title="Antigravity (agy)" checkedAt={agy.data?.checkedAt} tz={tz}>
          {agyErr && <Alert tone="danger">{agyErr.message}</Alert>}
          {agy.isPending && !agyErr && <p className="text-sm text-muted-foreground">Mengecek…</p>}
          {agy.data && (
            <div className="grid gap-4 md:grid-cols-2">
              {(agy.data.pools ?? []).map((p) => (
                <UsageBar key={p.name} title={p.name} window={p} tz={tz} note={modelsNote(p.models ?? [])} />
              ))}
            </div>
          )}
        </Section>
      )}
    </div>
  )
}
