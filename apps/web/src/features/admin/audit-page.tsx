import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { useTimezone } from '@/features/findbugs/queries'
import { formatDateTime } from '@/lib/format'
import { useAudit } from './queries'

const actionLabel: Record<string, string> = {
  'vpn.connect': 'VPN connect',
  'vpn.callback': 'VPN callback',
  'vpn.disconnect': 'VPN disconnect',
  'splunk.reauth': 'Splunk re-auth',
  'splunk.password': 'Splunk password SSO diganti',
  'user.create': 'Tambah user',
  'user.update': 'Ubah user',
  'user.passwd': 'Reset password (CLI)',
  'repo.clone': 'Clone repo',
  'repo.delete': 'Hapus repo',
  'repo.session_start': 'Mulai sesi rc',
  'repo.session_stop': 'Stop sesi rc',
  'note.create': 'Tambah rekomendasi',
  'note.update': 'Ubah rekomendasi',
  'note.archive': 'Arsipkan rekomendasi',
}

function resultTone(r: string) {
  if (r === 'ok' || r === 'connected') return 'success' as const
  if (r === 'error' || r === 'failed') return 'danger' as const
  return 'neutral' as const
}

export function AuditPage() {
  const tz = useTimezone()
  const audit = useAudit()
  return (
    <div className="grid gap-4">
      <PageHeader title="Audit log" description="Siapa menyambungkan VPN, login ulang Splunk, atau mengubah user. 200 entri terbaru." />
      <Card>
        <CardContent>
          {audit.error && <Alert tone="danger">{audit.error.message}</Alert>}
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
                  <th className="py-2 pr-3 font-medium">Waktu</th>
                  <th className="py-2 pr-3 font-medium">User</th>
                  <th className="py-2 pr-3 font-medium">Aksi</th>
                  <th className="py-2 pr-3 font-medium">Hasil</th>
                  <th className="py-2 font-medium">Detail</th>
                </tr>
              </thead>
              <tbody>
                {(audit.data ?? []).map((e) => (
                  <tr key={e.id} className="border-b last:border-0 align-top">
                    <td className="whitespace-nowrap py-2 pr-3 text-muted-foreground">{formatDateTime(e.at, tz)}</td>
                    <td className="py-2 pr-3">{e.username || 'sistem'}</td>
                    <td className="py-2 pr-3">{actionLabel[e.action] ?? e.action}</td>
                    <td className="py-2 pr-3">
                      <Badge tone={resultTone(e.result)}>{e.result}</Badge>
                    </td>
                    <td className="max-w-md break-words py-2 font-mono text-xs text-muted-foreground">{e.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {audit.data?.length === 0 && <p className="text-sm text-muted-foreground">Belum ada aktivitas.</p>}
        </CardContent>
      </Card>
    </div>
  )
}
