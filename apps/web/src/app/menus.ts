import { FileText, Inbox, Plug, Wrench, type LucideIcon } from 'lucide-react'
import type { Schemas, User } from '@/lib/api'

export type Menu = Schemas['User']['menus'][number]

/** Menus an Engineer can grant per QA, in nav order. */
export const menus: { key: Menu; label: string; to: string; icon: LucideIcon; hint: string }[] = [
  { key: 'investigasi', label: 'Investigasi', to: '/jobs', icon: Inbox, hint: 'Submit dan histori job' },
  { key: 'koneksi', label: 'Koneksi', to: '/koneksi', icon: Plug, hint: 'VPN dan Splunk' },
  { key: 'laporan', label: 'Laporan', to: '/laporan', icon: FileText, hint: 'Knowledge base trace' },
  { key: 'tools', label: 'Tools', to: '/tools', icon: Wrench, hint: 'Tools MyTelkomsel' },
]

export const allMenus = menus.map((m) => m.key)

export function hasMenu(user: User | null | undefined, m: Menu) {
  return Boolean(user && (user.role === 'engineer' || user.menus.includes(m)))
}
