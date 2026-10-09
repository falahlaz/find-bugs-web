import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, RouterProvider } from 'react-router'
import { ApiError } from '@/lib/api'
import { adminRoutes } from '@/features/admin/routes'
import { connectionRoutes } from '@/features/connection/routes'
import { findbugsRoutes } from '@/features/findbugs/routes'
import { mrsRoutes } from '@/features/mrs/routes'
import { notesRoutes } from '@/features/notes/routes'
import { reportsRoutes } from '@/features/reports/routes'
import { reposRoutes } from '@/features/repos/routes'
import { toolsRoutes } from '@/features/tools/routes'
import { AuthProvider } from './auth'
import { Layout } from './layout'
import { LoginPage } from './login-page'
import { NotFound } from './not-found'

const queryClient: QueryClient = new QueryClient({
  // An Engineer changed this QA's menus: refresh the user so the nav follows.
  queryCache: new QueryCache({
    onError: (e) => {
      if (e instanceof ApiError && e.code === 'menu_forbidden') void queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  }),
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
      ...mrsRoutes,
      ...reportsRoutes,
      ...notesRoutes,
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
