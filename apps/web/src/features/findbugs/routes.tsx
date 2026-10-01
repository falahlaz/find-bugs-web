import { Placeholder } from '@/app/placeholder'

export const findbugsRoutes = [
  { index: true, element: <Placeholder title="Submit investigasi" phase={2} /> },
  { path: 'jobs', element: <Placeholder title="Histori investigasi" phase={2} /> },
  { path: 'jobs/:id', element: <Placeholder title="Detail job" phase={2} /> },
]
