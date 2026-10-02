import { Database, Shield } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { StatusDot } from '@/app/shell/shared'
import { useHealth } from '@/app/shell/use-health'
import { useSystemStatus } from '@/app/use-system-status'
import { Button } from '@/components/ui/button'
import { useSplunkReauth } from '@/features/splunk/queries'
import { formatDuration, formatShort } from '@/lib/format'
import { cn } from '@/lib/utils'
import { jobSteps } from './pipeline'
import { PAGE_SIZE, useJobs, useTimezone } from './queries'
import { isFinal, isTrouble, statusInfo, type Job, type JobStatus } from './status'
import { StatusBadge } from './status-badge'
import { SubmitForm } from './submit-form'

function Tile({ className, children }: { className?: string; children: ReactNode }) {
  return <section className={cn('flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-5', className)}>{children}</section>
}

function TileLabel({ icon, children, right }: { icon?: ReactNode; children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 text-xs font-medium text-muted-foreground">
      <span className="flex items-center gap-1.5">
        {icon}
        {children}
      </span>
      {right}
    </div>
  )
}

const segClass = { done: 'bg-ok', skip: 'bg-ok/50', run: 'bg-info animate-pulse', wait: 'bg-warn animate-pulse', fail: 'bg-bad', idle: 'bg-secondary' }

function daysAgo(n: number) {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}

/** C · Bento Live: everything a QA needs at a glance on one screen. */
export function BentoHome() {
  const { user } = useAuth()
  const tz = useTimezone()
  const health = useHealth()
  const system = useSystemStatus()
  const reauth = useSplunkReauth()
  const engineer = user?.role === 'engineer'
  const mine = useJobs({ mine: '1' })
  const week = useJobs({ ...(engineer ? {} : { mine: '1' as const }), from: daysAgo(6) })

  const myJobs = mine.data ?? []
  const active = myJobs.find((j) => !isFinal(j.status as JobStatus)) ?? myJobs[0]
  const vpn = system.data?.vpn
  const splunk = system.data?.splunk

  // Last 7 days by outcome.
  const days = Array.from({ length: 7 }, (_, i) => daysAgo(6 - i))
  const dayKey = (iso: string) => new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(iso))
  const buckets = days.map((d) => {
    const js = (week.data ?? []).filter((j) => dayKey(j.queuedAt) === d)
    const s = (j: Job) => j.status as JobStatus
    return {
      day: new Intl.DateTimeFormat('id-ID', { weekday: 'short' }).format(new Date(`${d}T12:00:00`)),
      done: js.filter((j) => s(j) === 'DONE').length,
      none: js.filter((j) => s(j) === 'NO_LOGS').length,
      bad: js.filter((j) => isTrouble(s(j))).length,
    }
  })
  const weekTotal = (week.data ?? []).length
  const peak = Math.max(1, ...buckets.map((b) => b.done + b.none + b.bad))

  return (
    <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
      <section className="flex flex-col gap-4 rounded-xl bg-hero p-5 text-hero-foreground max-md:hidden md:col-span-2 lg:row-span-2">
        <h1 className="font-display text-2xl font-bold tracking-tight">Investigasi baru</h1>
        <SubmitForm variant="hero" idPrefix="hero" />
      </section>

      <Tile className="max-md:order-2">
        <TileLabel icon={<Shield className="size-3.5" />} right={<StatusDot ok={health.vpnOk} />}>
          VPN
        </TileLabel>
        <p className={cn('font-display text-[26px] leading-none font-bold tracking-tight', health.vpnOk ? 'text-ok' : 'text-bad')}>{health.vpnLabel}</p>
        <p className="text-xs text-muted-foreground">
          {vpn?.connectedAt ? `Sejak ${formatDuration(vpn.connectedAt)} lalu${vpn.connectedBy ? ` oleh ${vpn.connectedBy}` : ''}` : 'Sesi dipakai bersama semua user.'}
        </p>
        <Link to="/koneksi#vpn" className="mt-auto text-xs font-medium text-primary hover:underline">
          Kelola VPN →
        </Link>
      </Tile>

      <Tile className="max-md:order-3">
        <TileLabel icon={<Database className="size-3.5" />} right={<StatusDot ok={health.splunkOk} busy={health.splunkBusy} />}>
          Splunk
        </TileLabel>
        <p className={cn('font-display text-[26px] leading-none font-bold tracking-tight', health.splunkBusy ? 'text-warn' : health.splunkOk ? 'text-ok' : 'text-bad')}>
          {health.splunkLabel}
        </p>
        <p className="text-xs text-muted-foreground">
          {splunk?.session.savedAt ? `Login ${formatDuration(splunk.session.savedAt)} lalu` : 'Belum ada sesi tersimpan.'}
        </p>
        {health.splunkOk === false && !health.splunkBusy ? (
          <Button size="sm" className="mt-auto self-start rounded-full" disabled={!health.vpnOk || reauth.isPending} onClick={() => reauth.mutate()}>
            Re-auth
          </Button>
        ) : (
          <Link to="/koneksi#splunk" className="mt-auto text-xs font-medium text-primary hover:underline">
            Detail sesi →
          </Link>
        )}
      </Tile>

      {active ? (
        <Link
          to={`/jobs/${active.id}`}
          className="flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-5 transition-[transform,box-shadow] duration-200 hover:-translate-y-0.5 hover:shadow-[0_14px_34px_-16px_rgb(20_30_80/0.35)] max-md:order-1 md:col-span-2"
        >
          <TileLabel right={<span className="font-mono">#{active.id}</span>}>{isFinal(active.status as JobStatus) ? 'Job terakhir saya' : 'Job aktif'}</TileLabel>
          <span className="flex flex-wrap items-center justify-between gap-2">
            <span className="min-w-0 truncate font-mono text-sm font-medium">{active.transactionId}</span>
            <StatusBadge status={active.status as JobStatus} />
          </span>
          <span className="grid grid-cols-5 gap-1" aria-hidden>
            {jobSteps(active).map((s) => (
              <span key={s.key} className={cn('h-1.5 rounded-full', segClass[s.state])} />
            ))}
          </span>
          <span className="text-xs text-muted-foreground">
            {statusInfo[active.status as JobStatus].label} · {active.environment} · {formatShort(active.queuedAt, tz)}
          </span>
        </Link>
      ) : (
        <Tile className="max-md:order-1 md:col-span-2">
          <TileLabel>Job aktif</TileLabel>
          <p className="text-sm text-muted-foreground">Belum ada job. Tekan + untuk mulai investigasi.</p>
        </Tile>
      )}

      <Tile className="max-md:order-4">
        <TileLabel>Antrean</TileLabel>
        <p className="font-display text-[34px] leading-none font-bold tabular-nums">
          {health.queue.active}
          <span className="text-base text-muted-foreground">/{health.queue.max}</span>
        </p>
        <div className="flex flex-wrap gap-1" aria-hidden>
          {Array.from({ length: Math.min(health.queue.max, 20) }, (_, i) => (
            <span key={i} className={cn('size-3 rounded-[4px] border', i < health.queue.active ? 'border-primary bg-primary' : 'bg-secondary')} />
          ))}
        </div>
      </Tile>

      <Tile className="max-md:order-5">
        <TileLabel right={<span className="tabular-nums">{weekTotal === PAGE_SIZE ? `${PAGE_SIZE}+` : weekTotal} job</span>}>7 hari terakhir</TileLabel>
        <div className="grid h-20 grid-cols-7 items-end gap-1.5" role="img" aria-label="Jumlah job per hari selama 7 hari terakhir">
          {buckets.map((b, i) => (
            <div key={i} className="flex h-full flex-col-reverse gap-0.5">
              {(
                [
                  ['done', 'bg-ok'],
                  ['none', 'bg-neu'],
                  ['bad', 'bg-bad'],
                ] as const
              ).map(([k, c]) =>
                b[k] ? <span key={k} className={cn('rounded-[3px]', c)} style={{ height: `${(b[k] / peak) * 100}%` }} title={`${b.day}: ${b[k]}`} /> : null,
              )}
              {b.done + b.none + b.bad === 0 && <span className="h-0.5 rounded-full bg-secondary" />}
            </div>
          ))}
        </div>
        <div className="grid grid-cols-7 gap-1.5 text-center text-[10px] text-muted-foreground">
          {buckets.map((b, i) => (
            <span key={i}>{b.day}</span>
          ))}
        </div>
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <span className="size-2 rounded-sm bg-ok" />
            Selesai
          </span>
          <span className="flex items-center gap-1">
            <span className="size-2 rounded-sm bg-neu" />
            Tanpa log
          </span>
          <span className="flex items-center gap-1">
            <span className="size-2 rounded-sm bg-bad" />
            Gagal
          </span>
        </div>
      </Tile>

      <Tile className="max-md:order-6 md:col-span-2">
        <TileLabel
          right={
            <Link to="/jobs" className="font-medium text-primary hover:underline">
              Lihat semua
            </Link>
          }
        >
          Terbaru
        </TileLabel>
        <div className="grid">
          {myJobs.slice(0, 4).map((j) => (
            <Link key={j.id} to={`/jobs/${j.id}`} className="flex items-center gap-3 rounded-lg border-t px-1.5 py-2 first:border-t-0 hover:bg-secondary">
              <span className="min-w-0 flex-1 truncate font-mono text-[12.5px]">{j.transactionId}</span>
              <span className="text-xs text-muted-foreground tabular-nums">{formatShort(j.queuedAt, tz)}</span>
              <StatusBadge status={j.status as JobStatus} />
            </Link>
          ))}
          {myJobs.length === 0 && <p className="text-sm text-muted-foreground">Belum ada job.</p>}
        </div>
      </Tile>
    </div>
  )
}
