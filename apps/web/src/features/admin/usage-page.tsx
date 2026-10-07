import { useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { useTimezone } from '@/features/findbugs/queries'
import { api, unwrap, type Schemas } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'

function fetchUsage(refresh: boolean) {
  return api.GET('/api/usage', { params: { query: refresh ? { refresh: '1' } : {} } }).then(unwrap)
}

/** "2 jam 13 mnt lagi" until iso. */
function untilLabel(iso: string) {
  const m = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60_000))
  if (m < 60) return `${m} mnt lagi`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} jam ${m % 60} mnt lagi`
  return `${Math.floor(h / 24)} hari ${h % 24} jam lagi`
}

function UsageBar({ title, window, tz }: { title: string; window: Schemas['Window'] | null; tz: string }) {
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
      </CardContent>
    </Card>
  )
}

export function UsagePage() {
  const tz = useTimezone()
  const qc = useQueryClient()
  const usage = useQuery({ queryKey: ['usage'], queryFn: () => fetchUsage(false), refetchInterval: 5 * 60_000 })
  const refresh = useQuery({ queryKey: ['usage', 'refresh'], queryFn: () => fetchUsage(true), enabled: false })
  const onRefresh = async () => {
    const r = await refresh.refetch()
    if (r.data) qc.setQueryData(['usage'], r.data)
  }
  const data = usage.data
  const err = refresh.error ?? usage.error
  return (
    <div className="grid gap-4">
      <PageHeader
        title="Pemakaian Claude"
        description="Limit langganan Claude yang dipakai analyzer, sama seperti di claude.ai. Dicek ulang tiap 5 menit."
        actions={
          <Button variant="outline" size="sm" onClick={onRefresh} disabled={refresh.isFetching || usage.isFetching}>
            <RefreshCw className={cn(refresh.isFetching && 'animate-spin')} /> Cek sekarang
          </Button>
        }
      />
      {err && <Alert tone="danger">{err.message}</Alert>}
      {data?.status === 'rejected' && <Alert tone="danger">Limit sudah habis; analyzer akan gagal sampai reset.</Alert>}
      {usage.isPending && <p className="text-sm text-muted-foreground">Mengecek…</p>}
      {data && (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <UsageBar title="Sesi 5 jam" window={data.fiveHour} tz={tz} />
            <UsageBar title="Mingguan" window={data.sevenDay} tz={tz} />
          </div>
          <p className="text-xs text-muted-foreground">Terakhir dicek {formatDateTime(data.checkedAt, tz)}</p>
        </>
      )}
    </div>
  )
}
