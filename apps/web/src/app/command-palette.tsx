import { Clock, Database, FileText, Plug, Plus, ScrollText, Search, Users, type LucideIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { setListFilters } from '@/features/findbugs/list-filters'
import { useJobs } from '@/features/findbugs/queries'
import { statusInfo, type JobStatus } from '@/features/findbugs/status'
import { useSplunkReauth } from '@/features/splunk/queries'
import { cn } from '@/lib/utils'
import { useAuth } from './auth-context'
import { setPaletteOpen as setOpen, togglePalette, usePaletteOpen } from './palette-store'

type Item = { key: string; label: string; hint: string; icon: LucideIcon; mono?: boolean; run: () => void }

/** Subsequence match, so "trx8f" finds "TRX-8F3A…". */
function fuzzy(text: string, q: string) {
  let i = 0
  for (const ch of text) {
    if (ch === q[i]) i++
    if (i === q.length) return true
  }
  return q.length === 0
}

/** Global search and actions; Ctrl/⌘+K anywhere toggles it. */
export function CommandPalette() {
  const isOpen = usePaletteOpen()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        togglePalette()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  // Mounted only while open, so the query starts empty every time.
  return isOpen ? <PaletteDialog /> : null
}

function PaletteDialog() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const reauth = useSplunkReauth()
  const jobs = useJobs({})
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)

  const groups = useMemo(() => {
    const go = (to: string) => () => {
      setOpen(false)
      navigate(to)
    }
    const actions: Item[] = [
      { key: 'new', label: 'Investigasi baru', hint: 'Form submit', icon: Plus, run: go('/') },
      { key: 'jobs', label: 'Daftar investigasi', hint: 'Semua job', icon: Clock, run: go('/jobs') },
      { key: 'conn', label: 'Koneksi VPN & Splunk', hint: 'Status sistem', icon: Plug, run: go('/koneksi') },
      {
        key: 'reauth',
        label: 'Re-auth Splunk',
        hint: 'Kirim push 2FA',
        icon: Database,
        run: () => {
          setOpen(false)
          reauth.mutate()
          navigate('/koneksi#splunk')
        },
      },
    ]
    if (user?.role === 'engineer') {
      actions.push(
        { key: 'users', label: 'Kelola user', hint: 'Engineer', icon: Users, run: go('/users') },
        { key: 'audit', label: 'Audit log', hint: 'Engineer', icon: ScrollText, run: go('/audit') },
      )
    }
    const q = query.trim().toLowerCase()
    const match = (i: Item) => !q || fuzzy(i.label.toLowerCase(), q) || i.hint.toLowerCase().includes(q)
    const jobItems: Item[] = (jobs.data ?? []).map((j) => ({
      key: `job-${j.id}`,
      label: j.transactionId,
      hint: `#${j.id} · ${j.environment} · ${statusInfo[j.status as JobStatus].label}`,
      icon: FileText,
      mono: true,
      run: go(`/jobs/${j.id}`),
    }))
    const out: [string, Item[]][] = [
      ['Aksi', actions.filter(match)],
      [q ? 'Job' : 'Job terbaru', jobItems.filter(match).slice(0, q ? 8 : 5)],
    ]
    if (q.length >= 3)
      out.push([
        'Histori',
        [{ key: 'search', label: `Cari "${query.trim()}" di histori`, hint: 'Filter transaction ID', icon: Search, run: () => {
              setListFilters({ transactionId: query.trim(), status: undefined })
              go('/jobs')()
            }, }],
      ])
    return out.filter(([, items]) => items.length)
  }, [query, jobs.data, user, navigate, reauth])

  const flat = groups.flatMap(([, items]) => items)

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((a) => (a + 1) % Math.max(flat.length, 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => (a - 1 + flat.length) % Math.max(flat.length, 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      flat[active]?.run()
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  let n = 0
  return (
    <div className="fixed inset-0 z-[60]">
      <div className="absolute inset-0 bg-black/45 backdrop-blur-[2px]" onClick={() => setOpen(false)} aria-hidden />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="absolute top-[10dvh] left-1/2 w-[min(580px,calc(100%-24px))] -translate-x-1/2 overflow-hidden rounded-xl border bg-card shadow-[0_30px_80px_-20px_rgb(0_0_0/0.55)]"
      >
        <div className="animate-pop">
          <div className="flex items-center gap-3 border-b px-4 py-3 text-muted-foreground">
            <Search className="size-4" />
            <input
              autoFocus
              value={query}
              onChange={(e) => {
                setQuery(e.target.value)
                setActive(0)
              }}
              onKeyDown={onKeyDown}
              placeholder="Ketik perintah atau transaction ID…"
              aria-label="Cari perintah atau job"
              className="min-w-0 flex-1 bg-transparent text-[15px] text-foreground outline-none placeholder:text-muted-foreground"
            />
            <kbd className="kbd">Esc</kbd>
          </div>
          <div className="max-h-[50dvh] overflow-y-auto p-1.5">
            {groups.length === 0 && <p className="px-3 py-6 text-sm text-muted-foreground">Tidak ada hasil untuk "{query}".</p>}
            {groups.map(([title, items]) => (
              <div key={title}>
                <div className="px-3 pt-2 pb-1 text-[11px] font-medium text-muted-foreground">{title}</div>
                {items.map((item) => {
                  const idx = n++
                  return (
                    <button
                      key={item.key}
                      type="button"
                      onMouseEnter={() => setActive(idx)}
                      onClick={item.run}
                      className={cn('flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm', idx === active && 'bg-secondary')}
                    >
                      <item.icon className="size-4 shrink-0 text-muted-foreground" />
                      <span className={cn('min-w-0 truncate', item.mono && 'font-mono text-[13px]')}>{item.label}</span>
                      <span className="ml-auto shrink-0 text-xs text-muted-foreground">{item.hint}</span>
                    </button>
                  )
                })}
              </div>
            ))}
          </div>
          <div className="flex gap-4 border-t px-4 py-2 text-[11px] text-muted-foreground">
            <span>
              <kbd className="kbd">↑</kbd> <kbd className="kbd">↓</kbd> pilih
            </span>
            <span>
              <kbd className="kbd">↵</kbd> buka
            </span>
            <span className="max-sm:hidden">
              <kbd className="kbd">Ctrl K</kbd> buka/tutup
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}
