import type { ReactNode } from 'react'
import { useAuth } from '@/app/auth-context'
import { Alert } from '@/components/ui/alert'

export function EngineerOnly({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  if (user?.role !== 'engineer') return <Alert tone="danger">Halaman ini khusus Engineer.</Alert>
  return <>{children}</>
}
