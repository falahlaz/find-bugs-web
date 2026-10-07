import { AuditPage } from './audit-page'
import { EngineerOnly } from './engineer-only'
import { UsagePage } from './usage-page'
import { UsersPage } from './users-page'

export const adminRoutes = [
  {
    path: 'users',
    element: (
      <EngineerOnly>
        <UsersPage />
      </EngineerOnly>
    ),
  },
  {
    path: 'audit',
    element: (
      <EngineerOnly>
        <AuditPage />
      </EngineerOnly>
    ),
  },
  {
    path: 'usage',
    element: (
      <EngineerOnly>
        <UsagePage />
      </EngineerOnly>
    ),
  },
]
