import { useEffect, useState } from 'react'

const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches

/**
 * Writes `text` out a few characters at a time when `enabled`, so a diagnosis
 * that just arrived reads like it is being produced. Remount (key) to replay.
 */
export function useTypewriter(text: string, enabled: boolean, charsPerTick = 3) {
  const [shown, setShown] = useState(() => (enabled && !reducedMotion() ? 0 : Infinity))
  useEffect(() => {
    if (shown >= text.length) return
    const t = setTimeout(() => setShown((n) => n + charsPerTick), 22)
    return () => clearTimeout(t)
  }, [shown, text.length, charsPerTick])
  return { text: text.slice(0, shown), done: shown >= text.length }
}
