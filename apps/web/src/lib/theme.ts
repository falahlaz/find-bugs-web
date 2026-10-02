import { useCallback, useEffect, useSyncExternalStore } from 'react'

export type Theme = 'light' | 'dark' | 'system'

// Same key as public/theme-init.js, which applies it before first paint.
const storageKey = 'theme'
const darkQuery = '(prefers-color-scheme: dark)'
const listeners = new Set<() => void>()

function readTheme(): Theme {
  try {
    const t = localStorage.getItem(storageKey)
    if (t === 'light' || t === 'dark') return t
  } catch {
    // Storage can be blocked; fall back to the OS setting.
  }
  return 'system'
}

function applyTheme(theme: Theme) {
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia(darkQuery).matches)
  document.documentElement.classList.toggle('dark', dark)
}

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

/** Light / dark / follow-the-OS preference, persisted in this browser. */
export function useTheme() {
  const theme = useSyncExternalStore(subscribe, readTheme)

  const setTheme = useCallback((t: Theme) => {
    try {
      if (t === 'system') localStorage.removeItem(storageKey)
      else localStorage.setItem(storageKey, t)
    } catch {
      // Not persisted; still applied for this page view.
    }
    applyTheme(t)
    listeners.forEach((l) => l())
  }, [])

  useEffect(() => {
    applyTheme(theme)
    if (theme !== 'system') return
    const mq = window.matchMedia(darkQuery)
    const onChange = () => applyTheme('system')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  return { theme, setTheme }
}
