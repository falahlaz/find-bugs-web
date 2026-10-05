import { EngineerOnly } from '@/features/admin/engineer-only'
import { ReposPage } from './repos-page'

export const reposRoutes = [
  {
    path: 'repos',
    element: (
      <EngineerOnly>
        <ReposPage />
      </EngineerOnly>
    ),
  },
]
