import { MenuGuard } from '@/app/menu-guard'
import { ReportPage } from './report-page'
import { ReportsPage } from './reports-page'

export const reportsRoutes = [
  { path: 'laporan', element: <MenuGuard menu="laporan"><ReportsPage /></MenuGuard> },
  { path: 'laporan/:repo/:slug', element: <MenuGuard menu="laporan"><ReportPage /></MenuGuard> },
]
