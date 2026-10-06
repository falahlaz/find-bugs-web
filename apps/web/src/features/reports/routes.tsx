import { ReportPage } from './report-page'
import { ReportsPage } from './reports-page'

export const reportsRoutes = [
  { path: 'laporan', element: <ReportsPage /> },
  { path: 'laporan/:repo/:slug', element: <ReportPage /> },
]
