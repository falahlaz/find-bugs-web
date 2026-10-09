import { EngineerOnly } from '@/features/admin/engineer-only'
import { NotesPage } from './notes-page'

export const notesRoutes = [
  {
    path: 'rekomendasi',
    element: (
      <EngineerOnly>
        <NotesPage />
      </EngineerOnly>
    ),
  },
]
