import { Clock, FolderGit2, Gauge, GitMerge, Lightbulb, Plug, Plus, ScrollText, Users, type LucideIcon } from 'lucide-react'

export type NavItem = { to: string; label: string; short: string; icon: LucideIcon; end?: boolean }

export const primaryNav: NavItem[] = [
  { to: '/', label: 'Investigasi baru', short: 'Baru', icon: Plus, end: true },
  { to: '/jobs', label: 'Histori', short: 'Histori', icon: Clock },
  { to: '/koneksi', label: 'Koneksi', short: 'Koneksi', icon: Plug },
]

export const engineerNav: NavItem[] = [
  { to: '/repos', label: 'Repo', short: 'Repo', icon: FolderGit2 },
  { to: '/mrs', label: 'MR Triage', short: 'MR', icon: GitMerge },
  { to: '/rekomendasi', label: 'Rekomendasi engineer', short: 'Saran', icon: Lightbulb },
  { to: '/users', label: 'User', short: 'User', icon: Users },
  { to: '/audit', label: 'Audit log', short: 'Audit', icon: ScrollText },
  { to: '/usage', label: 'Pemakaian AI', short: 'Usage', icon: Gauge },
]

export function initials(name: string) {
  return name.slice(0, 1).toUpperCase()
}
