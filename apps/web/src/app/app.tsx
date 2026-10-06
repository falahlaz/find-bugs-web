import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, RouterProvider } from 'react-router'
import { adminRoutes } from '@/features/admin/routes'
import { connectionRoutes } from '@/features/connection/routes'
import { findbugsRoutes } from '@/features/findbugs/routes'
import { reportsRoutes } from '@/features/reports/routes'
import { reposRoutes } from '@/features/repos/routes'
import { toolsRoutes } from '@/features/tools/routes'
import { AuthProvider } from './auth'
import { Layout } from './layout'
import { LoginPage } from './login-page'
import { NotFound } from './not-found'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true } },
})

// Each feature contributes its own routes so new features are new folders.
const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    path: '/',
    element: <Layout />,
    children: [
      ...findbugsRoutes,
      ...connectionRoutes,
      ...adminRoutes,
      ...reposRoutes,
      ...reportsRoutes,
      ...toolsRoutes,
      { path: '*', element: <NotFound /> },
    ],
  },
])

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>
  )
}
