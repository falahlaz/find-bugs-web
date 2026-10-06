import { Navigate } from 'react-router'
import { MenuGuard } from '@/app/menu-guard'
import { ConnectionPage } from './connection-page'

export const connectionRoutes = [
  { path: 'koneksi', element: <MenuGuard menu="koneksi"><ConnectionPage /></MenuGuard> },
  // Old separate pages; keep bookmarks and links working.
  { path: 'vpn', element: <Navigate to="/koneksi#vpn" replace /> },
  { path: 'splunk', element: <Navigate to="/koneksi#splunk" replace /> },
]
