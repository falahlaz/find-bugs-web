import { MenuGuard } from '@/app/menu-guard'
import { JobPage } from './job-page'
import { JobsPage } from './jobs-page'
import { SubmitPage } from './submit-page'

export const findbugsRoutes = [
  { index: true, element: <MenuGuard menu="investigasi"><SubmitPage /></MenuGuard> },
  { path: 'jobs', element: <MenuGuard menu="investigasi"><JobsPage /></MenuGuard> },
  { path: 'jobs/:id', element: <MenuGuard menu="investigasi"><JobPage /></MenuGuard> },
]
