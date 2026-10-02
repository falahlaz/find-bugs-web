/** Formats an ISO timestamp in the server timezone (default Asia/Jakarta). */
export function formatDateTime(iso: string | null | undefined, timeZone = 'Asia/Jakarta') {
  if (!iso) return '–'
  return new Intl.DateTimeFormat('id-ID', {
    timeZone,
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(iso))
}

export function formatTime(iso: string | null | undefined, timeZone = 'Asia/Jakarta') {
  if (!iso) return '–'
  return new Intl.DateTimeFormat('id-ID', { timeZone, hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(iso))
}

/** "1m 05d" style duration between two timestamps (end defaults to now). */
export function formatDuration(startIso: string | null | undefined, endIso?: string | null) {
  if (!startIso) return '–'
  const ms = Math.max(0, (endIso ? new Date(endIso).getTime() : Date.now()) - new Date(startIso).getTime())
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s} dtk`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} mnt ${String(s % 60).padStart(2, '0')} dtk`
  return `${Math.floor(m / 60)} jam ${m % 60} mnt`
}

/** "10:41" for today, "1 Okt" otherwise; for dense lists. */
export function formatShort(iso: string | null | undefined, timeZone = 'Asia/Jakarta') {
  if (!iso) return '–'
  const d = new Date(iso)
  const day = (x: Date) => new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(x)
  if (day(d) === day(new Date())) return new Intl.DateTimeFormat('id-ID', { timeZone, hour: '2-digit', minute: '2-digit' }).format(d)
  return new Intl.DateTimeFormat('id-ID', { timeZone, day: 'numeric', month: 'short' }).format(d)
}
