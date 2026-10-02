import { useCallback, useSyncExternalStore } from 'react'

/**
 * Three UI directions from the revamp exploration. They share every page and
 * hook; only the shell (navigation) and a few page bodies differ, and the
 * tokens in index.css restyle the shared components.
 */
export type Design = 'command' | 'triage' | 'bento'

export const designs: { id: Design; label: string; short: string; hint: string }[] = [
  { id: 'command', label: 'Command', short: 'A', hint: 'Tenang, keyboard-first' },
  { id: 'triage', label: 'Triage', short: 'B', hint: 'Inbox investigasi' },
  { id: 'bento', label: 'Bento Live', short: 'C', hint: 'Beranda hidup' },
]

// Same key as public/theme-init.js, which applies it before first paint.
const storageKey = 'findbugs.design'
const fallback: Design = 'triage'
const listeners = new Set<() => void>()

function isDesign(v: unknown): v is Design {
  return v === 'command' || v === 'triage' || v === 'bento'
}

function readDesign(): Design {
  const attr = document.documentElement.dataset.design
  if (isDesign(attr)) return attr
  try {
    const d = localStorage.getItem(storageKey)
    if (isDesign(d)) return d
  } catch {
    // Storage can be blocked; use the default design.
  }
  return fallback
}

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

export function useDesign() {
  const design = useSyncExternalStore(subscribe, readDesign)
  const setDesign = useCallback((d: Design) => {
    try {
      localStorage.setItem(storageKey, d)
    } catch {
      // Not persisted; still applied for this page view.
    }
    document.documentElement.dataset.design = d
    listeners.forEach((l) => l())
  }, [])
  return { design, setDesign }
}
