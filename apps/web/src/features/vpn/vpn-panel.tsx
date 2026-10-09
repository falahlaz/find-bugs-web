import { useState, type FormEvent } from 'react'
import { useAuth } from '@/app/auth-context'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Row } from '@/components/ui/row'
import { Textarea } from '@/components/ui/textarea'
import { useTimezone } from '@/features/findbugs/queries'
import { formatDateTime, formatDuration } from '@/lib/format'
import { busyStates, useLoginUrl, useVpnCallback, useVpnConnect, useVpnDisconnect, useVpnLogs, useVpnStatus } from './queries'

const stateLabel: Record<string, string> = {
  IDLE: 'Idle',
  CONNECTING: 'Menyiapkan login',
  WAITING_CALLBACK: 'Menunggu login SSO',
  SUBMITTING: 'Mengirim callback',
  CONNECTED: 'Callback diterima',
  FAILED: 'Gagal',
}


/** Connection badge(s); shared with the summary on the Koneksi page. */
export function VpnBadge() {
  const { data: s } = useVpnStatus()
  if (!s) return null
  const busy = busyStates.includes(s.state)
  return (
    <>
      <Badge tone={s.healthy ? 'success' : 'danger'}>{s.healthy ? 'Tersambung' : 'Tidak tersambung'}</Badge>
      {busy && <Badge tone="info">{stateLabel[s.state]}</Badge>}
    </>
  )
}

export function VpnPanel() {
  const { user } = useAuth()
  const tz = useTimezone()
  const status = useVpnStatus()
  const connect = useVpnConnect()
  const callback = useVpnCallback()
  const disconnect = useVpnDisconnect()
  const [uri, setUri] = useState('')
  const [showLogs, setShowLogs] = useState(false)
  const logs = useVpnLogs(showLogs)

  const s = status.data
  const state = s?.state ?? 'IDLE'
  const inProgress = busyStates.includes(state)
  const someoneElse = inProgress && s?.operator && s.operator !== user?.username
  const loginUrl = useLoginUrl(inProgress && !someoneElse)
  // The link is only meaningful during the current attempt.
  const link = inProgress && !someoneElse ? (loginUrl.data?.url ?? null) : null

  function onCallback(e: FormEvent) {
    e.preventDefault()
    callback.mutate(uri.trim(), { onSuccess: () => setUri('') })
  }

  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center gap-3">
          <CardTitle>VPN GlobalProtect</CardTitle>
          <VpnBadge />
          <span className="ml-auto text-xs text-muted-foreground">Satu sesi VPN dipakai bersama semua user.</span>
        </CardHeader>
        <CardContent className="grid gap-2">
          {status.error && <Alert tone="danger">{status.error.message}</Alert>}
          {!s && status.isPending && <p className="text-sm text-muted-foreground">Memeriksa status…</p>}
          {s && (
            <>
              <Row label="Status GlobalProtect">
                <span className="font-mono text-xs">{s.gpStatus || '–'}</span>
              </Row>
              <Row label="Interface">{s.iface.length ? s.iface.join(', ') : '–'}</Row>
              <Row label="Reachability">
                {s.reach.length === 0
                  ? '–'
                  : s.reach.map((r) => (
                      <span key={r.host} className="mr-3 inline-flex items-center gap-1">
                        <span className={r.ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-destructive'}>{r.ok ? '●' : '○'}</span>
                        <span className="font-mono text-xs">{r.host}</span>
                      </span>
                    ))}
              </Row>
              {s.connectedBy && (
                <Row label="Disambungkan oleh">
                  {s.connectedBy} · {formatDateTime(s.connectedAt, tz)} ({formatDuration(s.connectedAt)} lalu)
                </Row>
              )}
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Sambungkan ulang</CardTitle>
          <CardDescription>Login memakai akun SSO kamu sendiri. Setelah tersambung, semua user langsung bisa memakai VPN ini.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-5">
          {someoneElse && <Alert tone="info">{s?.operator} sedang menyambungkan VPN. Tunggu sampai selesai.</Alert>}

          <ol className="grid gap-5 text-sm">
            <li className="grid gap-2">
              <div className="font-medium">1. Mulai koneksi</div>
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => connect.mutate()} disabled={connect.isPending || inProgress}>
                  {connect.isPending ? 'Memulai…' : 'Connect'}
                </Button>
                {s?.healthy && <span className="self-center text-muted-foreground">VPN sudah tersambung; Connect hanya perlu kalau sesi habis.</span>}
              </div>
              {connect.error && <Alert tone="danger">{connect.error.message}</Alert>}
            </li>

            <li className="grid gap-2">
              <div className="font-medium">2. Login SSO di browser ini</div>
              {link ? (
                <div className="flex flex-wrap items-center gap-2">
                  <a href={link} target="_blank" rel="noopener noreferrer" className="font-medium text-primary underline">
                    Buka halaman login SSO ↗
                  </a>
                  <span className="text-muted-foreground">
                    Setelah login, klik kanan link <i>"click here"</i> di halaman akhir lalu pilih <i>Copy link</i>.
                  </span>
                </div>
              ) : (
                <p className="text-muted-foreground">{inProgress && !someoneElse ? 'Menunggu link login dari GlobalProtect…' : 'Klik Connect dulu.'}</p>
              )}
            </li>

            <li>
              <form onSubmit={onCallback} className="grid gap-2">
                <Label htmlFor="callback" className="font-medium">
                  3. Paste link callback (dalam ±60 detik)
                </Label>
                <Textarea
                  id="callback"
                  rows={3}
                  className="font-mono text-xs"
                  placeholder="globalprotectcallback:..."
                  value={uri}
                  onChange={(e) => setUri(e.target.value)}
                  disabled={!link}
                  autoComplete="off"
                  spellCheck={false}
                />
                <div>
                  <Button type="submit" disabled={!link || !uri.trim().startsWith('globalprotectcallback:') || callback.isPending}>
                    {callback.isPending ? 'Mengirim…' : 'Kirim callback'}
                  </Button>
                </div>
                {callback.error && <Alert tone="danger">{callback.error.message}</Alert>}
                {callback.data && (
                  <Alert tone={callback.data.state === 'CONNECTED' ? 'info' : 'danger'}>
                    {callback.data.state === 'CONNECTED'
                      ? 'Callback diterima GlobalProtect. Status VPN akan berubah jadi Tersambung dalam beberapa detik.'
                      : `Callback gagal (exit ${callback.data.exitCode}). ${callback.data.stderr || callback.data.stdout}`}
                  </Alert>
                )}
              </form>
            </li>
          </ol>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center gap-2">
          <CardTitle>Lainnya</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4">
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={disconnect.isPending}
              onClick={() => {
                if (window.confirm('Putuskan VPN dan restart daemon GlobalProtect (gpd + gpa)? Semua job akan menunggu sampai ada yang connect lagi.')) disconnect.mutate()
              }}
            >
              Disconnect
            </Button>
            <Button variant="ghost" onClick={() => setShowLogs((v) => !v)}>
              {showLogs ? 'Sembunyikan log' : 'Lihat log (diredaksi)'}
            </Button>
          </div>
          {disconnect.error && <Alert tone="danger">{disconnect.error.message}</Alert>}
          {showLogs && (
            <pre className="max-h-96 overflow-auto rounded-md bg-muted p-3 text-xs">{(logs.data ?? []).join('\n') || 'Belum ada log.'}</pre>
          )}
          <p className="text-xs text-muted-foreground">
            Disconnect juga me-restart daemon GlobalProtect (<code>gpd</code> + <code>gpa</code>). Kalau Connect gagal karena sesi
            nyangkut ("already established" / "Retrieving configuration..."), klik Disconnect lalu Connect lagi.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
