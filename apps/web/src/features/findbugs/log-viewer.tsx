import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { cn } from '@/lib/utils'

type Level = 'ERROR' | 'WARN' | 'INFO' | 'OTHER'
const levels: Level[] = ['ERROR', 'WARN', 'INFO', 'OTHER']
const levelClass: Record<Level, string> = { ERROR: 'text-bad', WARN: 'text-warn', INFO: 'text-info', OTHER: 'text-muted-foreground' }

function levelOf(line: string): Level {
  if (/\b(ERROR|FATAL|ERR|SEVERE|CRITICAL)\b/i.test(line) || /"level"\s*:\s*"(error|fatal)"/i.test(line)) return 'ERROR'
  if (/\bWARN(ING)?\b/i.test(line) || /"level"\s*:\s*"warn/i.test(line)) return 'WARN'
  if (/\b(INFO|DEBUG)\b/i.test(line) || /"level"\s*:\s*"(info|debug)"/i.test(line)) return 'INFO'
  return 'OTHER'
}

function highlight(line: string, q: string) {
  if (!q) return line
  const parts = line.split(new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi'))
  return parts.map((p, i) =>
    i % 2 === 1 ? (
      <mark key={i} className="rounded-sm bg-warn/35 text-inherit">
        {p}
      </mark>
    ) : (
      p
    ),
  )
}

/** Redacted log lines with a text filter, level toggles and line numbers. */
export function LogViewer({ lines, className, maxHeight = 'max-h-96' }: { lines: string[]; className?: string; maxHeight?: string }) {
  const [q, setQ] = useState('')
  const [hidden, setHidden] = useState<Set<Level>>(new Set())
  const rows = useMemo(() => lines.map((text, i) => ({ n: i + 1, text, level: levelOf(text) })), [lines])
  const counts = useMemo(() => {
    const c: Record<Level, number> = { ERROR: 0, WARN: 0, INFO: 0, OTHER: 0 }
    rows.forEach((r) => c[r.level]++)
    return c
  }, [rows])
  const query = q.trim()
  const shown = rows.filter((r) => !hidden.has(r.level) && (!query || r.text.toLowerCase().includes(query.toLowerCase())))

  return (
    <div className={cn('overflow-hidden rounded-lg border bg-muted', className)}>
      <div className="flex flex-wrap items-center gap-1.5 border-b px-2.5 py-2">
        <label className="flex min-w-[140px] flex-1 items-center gap-1.5 text-muted-foreground">
          <Search className="size-3.5" />
          <span className="sr-only">Filter log</span>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Filter log"
            className="min-w-0 flex-1 bg-transparent py-1 font-mono text-xs text-foreground outline-none placeholder:font-sans"
          />
        </label>
        {levels
          .filter((l) => counts[l] > 0)
          .map((l) => (
            <button
              key={l}
              type="button"
              aria-pressed={!hidden.has(l)}
              onClick={() =>
                setHidden((h) => {
                  const n = new Set(h)
                  if (n.has(l)) n.delete(l)
                  else n.add(l)
                  return n
                })
              }
              className={cn(
                'rounded-full border px-2 py-0.5 font-mono text-[11px] transition-colors',
                hidden.has(l) ? 'text-muted-foreground/60 line-through' : 'bg-card text-foreground',
              )}
            >
              {l === 'OTHER' ? 'lain' : l}
              <span className="ml-1 opacity-60">{counts[l]}</span>
            </button>
          ))}
      </div>
      {shown.length === 0 ? (
        <p className="px-3 py-4 text-xs text-muted-foreground">Tidak ada baris yang cocok dengan filter.</p>
      ) : (
        <div className={cn('overflow-auto py-1.5 font-mono text-[11.5px] leading-[1.8]', maxHeight)}>
          {shown.map((r) => (
            <div key={r.n} className={cn('grid grid-cols-[2.2rem_max-content] gap-3 pr-4 hover:bg-secondary', r.level === 'ERROR' && 'bg-bad-bg/60')}>
              <span className="text-right text-muted-foreground/60 select-none">{r.n}</span>
              <span className={cn('whitespace-pre', r.level !== 'OTHER' && r.level !== 'INFO' && levelClass[r.level])}>{highlight(r.text, query)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
