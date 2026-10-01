import type { BadgeTone } from '@/components/ui/badge'
import type { Schemas } from '@/lib/api'

export type JobStatus = Schemas['Job']['status']
export type Job = Schemas['Job']
export type JobView = Schemas['JobView']

export const statusInfo: Record<JobStatus, { label: string; tone: BadgeTone; hint?: string }> = {
  QUEUED: { label: 'Antre', tone: 'neutral' },
  CHECKING_VPN: { label: 'Cek VPN', tone: 'info' },
  SEARCHING: { label: 'Mencari log', tone: 'info' },
  ANALYZING: { label: 'Analisis AI', tone: 'info' },
  WAITING_VPN: { label: 'Menunggu VPN', tone: 'warning', hint: 'VPN sedang tidak tersambung. Job lanjut otomatis begitu ada yang login ulang VPN.' },
  WAITING_SPLUNK: { label: 'Menunggu Splunk', tone: 'warning', hint: 'Sesi Splunk kedaluwarsa. Job lanjut otomatis setelah ada yang Re-auth di panel Splunk.' },
  DONE: { label: 'Selesai', tone: 'success' },
  NO_LOGS: { label: 'Log tidak ditemukan', tone: 'neutral' },
  FAILED: { label: 'Gagal', tone: 'danger' },
  CANCELLED: { label: 'Dibatalkan', tone: 'neutral' },
  EXPIRED: { label: 'Kedaluwarsa', tone: 'danger' },
}

export const allStatuses = Object.keys(statusInfo) as JobStatus[]

const finals: JobStatus[] = ['DONE', 'NO_LOGS', 'FAILED', 'CANCELLED', 'EXPIRED']
export const isFinal = (s: JobStatus) => finals.includes(s)
export const isPending = (s: JobStatus) => s === 'QUEUED' || s === 'WAITING_VPN' || s === 'WAITING_SPLUNK'

export const severityTone: Record<string, BadgeTone> = {
  low: 'neutral',
  medium: 'warning',
  high: 'danger',
  critical: 'danger',
}
