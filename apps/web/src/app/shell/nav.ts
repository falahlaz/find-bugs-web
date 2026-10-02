import { Clock, Plug, Plus, ScrollText, Users, type LucideIcon } from 'lucide-react'

export type NavItem = { to: string; label: string; short: string; icon: LucideIcon; end?: boolean }

export const primaryNav: NavItem[] = [
  { to: '/', label: 'Investigasi baru', short: 'Baru', icon: Plus, end: true },
  { to: '/jobs', label: 'Histori', short: 'Histori', icon: Clock },
  { to: '/koneksi', label: 'Koneksi', short: 'Koneksi', icon: Plug },
]

export const engineerNav: NavItem[] = [
  { to: '/users', label: 'User', short: 'User', icon: Users },
  { to: '/audit', label: 'Audit log', short: 'Audit', icon: ScrollText },
]

export function initials(name: string) {
  return name.slice(0, 1).toUpperCase()
}
