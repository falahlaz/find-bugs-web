import { Navigate } from 'react-router'
import { ConnectionPage } from './connection-page'

export const connectionRoutes = [
  { path: 'koneksi', element: <ConnectionPage /> },
  // Old separate pages; keep bookmarks and links working.
  { path: 'vpn', element: <Navigate to="/koneksi#vpn" replace /> },
  { path: 'splunk', element: <Navigate to="/koneksi#splunk" replace /> },
]
