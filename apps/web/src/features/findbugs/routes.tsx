import { JobPage } from './job-page'
import { JobsPage } from './jobs-page'
import { SubmitPage } from './submit-page'

export const findbugsRoutes = [
  { index: true, element: <SubmitPage /> },
  { path: 'jobs', element: <JobsPage /> },
  { path: 'jobs/:id', element: <JobPage /> },
]
