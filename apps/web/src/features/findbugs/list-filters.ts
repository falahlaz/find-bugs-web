import { useSyncExternalStore } from 'react'
import type { JobFilters } from './queries'

/**
 * Filters for the job inbox in the shell. Kept outside React state so they
 * survive moving between jobs and so the command palette can set them.
 * `mine` undefined means the role default (own jobs for QA, all for engineers).
 */
export type ListFilters = Omit<JobFilters, 'mine'> & { mine?: boolean }

// Old /jobs?transactionId=… links (bookmarks) seed the first state.
function fromUrl(): ListFilters {
  const q = new URLSearchParams(location.pathname === '/jobs' ? location.search : '')
  const pick = (k: string) => q.get(k) || undefined
  return { transactionId: pick('transactionId'), status: pick('status'), environment: pick('environment'), from: pick('from'), to: pick('to') }
}

let state: ListFilters = fromUrl()
const listeners = new Set<() => void>()

export function setListFilters(patch: Partial<ListFilters>) {
  state = { ...state, ...patch }
  listeners.forEach((l) => l())
}

const subscribe = (cb: () => void) => {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

export const useListFilters = () => useSyncExternalStore(subscribe, () => state)

