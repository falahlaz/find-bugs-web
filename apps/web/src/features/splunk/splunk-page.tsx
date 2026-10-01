import { useSystemStatus } from '@/app/use-system-status'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useTimezone } from '@/features/findbugs/queries'
import { formatDateTime, formatDuration } from '@/lib/format'
import { useSplunkReauth, useSplunkStatus } from './queries'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[10rem_1fr] gap-2 text-sm">
      <div className="text-muted-foreground">{label}</div>
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}

export function SplunkPage() {
  const tz = useTimezone()
  const status = useSplunkStatus()
  const system = useSystemStatus()
  const reauth = useSplunkReauth()
  const s = status.data
  const running = Boolean(s?.reauthing || s?.session.reauthRunning)
  const vpnUp = system.data?.vpn.healthy ?? false

  let badge = <Badge tone="neutral">Belum dicek</Badge>
  if (running) badge = <Badge tone="info">Menunggu 2FA</Badge>
  else if (s?.paused) badge = <Badge tone="danger">Kedaluwarsa · antrean dijeda</Badge>
  else if (s?.ok) badge = <Badge tone="success">Aktif</Badge>
  else if (s) badge = <Badge tone="warning">Tidak bisa dicek</Badge>

  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center gap-3">
          <CardTitle>Sesi Splunk</CardTitle>
          {badge}
        </CardHeader>
        <CardContent className="grid gap-2">
          {status.error && <Alert tone="danger">{status.error.message}</Alert>}
          {s && (
            <>
              <Row label="Sesi tersimpan">
                {s.session.loaded ? (
                  <>
                    {formatDateTime(s.session.savedAt, tz)} ({formatDuration(s.session.savedAt)} lalu)
                  </>
                ) : (
                  'Belum ada sesi'
                )}
              </Row>
              {s.session.missingCookies && s.session.missingCookies.length > 0 && (
                <Row label="Cookie hilang">{s.session.missingCookies.join(', ')}</Row>
              )}
              <Row label="Terakhir dicek">{s.checkedAt.startsWith('0001') ? '–' : formatDateTime(s.checkedAt, tz)}</Row>
              {s.detail && <Row label="Detail">{s.detail}</Row>}
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Login ulang (Re-auth)</CardTitle>
          <CardDescription>
            Server login otomatis memakai akun SSO Splunk yang tersimpan, lalu menunggu push 2FA di HP pemilik akun itu (maks 5 menit). Butuh VPN
            tersambung.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {!vpnUp && <Alert tone="warning">VPN belum tersambung. Sambungkan VPN dulu di panel VPN.</Alert>}
          <div>
            <Button onClick={() => reauth.mutate()} disabled={!vpnUp || running || reauth.isPending}>
              {running ? 'Menunggu approve 2FA…' : 'Re-auth'}
            </Button>
          </div>
          {reauth.error && <Alert tone="danger">{reauth.error.message}</Alert>}
          {running && <Alert tone="info">Minta pemilik akun Splunk approve push 2FA di HP-nya.</Alert>}
          {s?.session.lastReauthAt && !running && (
            <Alert tone={s.session.lastReauthOk ? 'info' : 'danger'}>
              Re-auth terakhir {formatDateTime(s.session.lastReauthAt, tz)}: {s.session.lastReauthOk ? 'berhasil' : 'gagal'}.
            </Alert>
          )}
          {s?.session.lastReauthLog && s.session.lastReauthLog.some(Boolean) && (
            <details>
              <summary className="cursor-pointer text-sm font-medium">Output login terakhir</summary>
              <pre className="mt-2 max-h-72 overflow-auto rounded-md bg-muted p-3 text-xs">{s.session.lastReauthLog.join('\n')}</pre>
            </details>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
