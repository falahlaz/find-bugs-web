import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Schemas, unwrap } from '@/lib/api'

export type EngineerNote = Schemas['NoteListItem']
export type NoteView = Schemas['NoteView']
export type NoteReview = Schemas['NoteReviewResponse']
export type NoteCheck = Schemas['NoteCheckRequest']
export type NoteSave = Schemas['NoteSaveRequest']
export type NoteState = 'active' | 'archived'

export function useNotes(state: NoteState) {
  return useQuery({
    queryKey: ['notes', state],
    queryFn: async () => unwrap(await api.GET('/api/notes', { params: { query: { state } } })).notes,
  })
}

export function useNoteVersions(id: number, enabled: boolean) {
  return useQuery({
    queryKey: ['note-versions', id],
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/notes/{id}/versions', { params: { path: { id } } })).versions,
  })
}

/** AI review of a note; nothing is saved. */
export function useCheckNote() {
  return useMutation({
    mutationFn: async (body: NoteCheck) => unwrap(await api.POST('/api/notes/check', { body })),
  })
}

/** Notes show on jobs, so a change refreshes every job and the note list. */
function useInvalidateNotes() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['job'] })
    void qc.invalidateQueries({ queryKey: ['notes'] })
    void qc.invalidateQueries({ queryKey: ['note-versions'] })
  }
}

export function useSaveNote() {
  const invalidate = useInvalidateNotes()
  return useMutation({
    mutationFn: async (body: NoteSave) => unwrap(await api.POST('/api/notes', { body })),
    onSuccess: invalidate,
  })
}

export function useArchiveNote() {
  const invalidate = useInvalidateNotes()
  return useMutation({
    mutationFn: async (id: number) => unwrap(await api.POST('/api/notes/{id}/archive', { params: { path: { id } } })),
    onSuccess: invalidate,
  })
}
