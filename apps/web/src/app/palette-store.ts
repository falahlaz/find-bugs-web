import { useSyncExternalStore } from 'react'

let open = false
const listeners = new Set<() => void>()

export function setPaletteOpen(v: boolean) {
  open = v
  listeners.forEach((l) => l())
}
export const openPalette = () => setPaletteOpen(true)
export const togglePalette = () => setPaletteOpen(!open)

const subscribe = (cb: () => void) => {
  listeners.add(cb)
  return () => listeners.delete(cb)
}
export const usePaletteOpen = () => useSyncExternalStore(subscribe, () => open)
