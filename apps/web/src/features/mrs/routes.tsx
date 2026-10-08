import { EngineerOnly } from '@/features/admin/engineer-only'
import { MRsPage } from './mrs-page'

export const mrsRoutes = [
  {
    path: 'mrs',
    element: (
      <EngineerOnly>
        <MRsPage />
      </EngineerOnly>
    ),
  },
]
