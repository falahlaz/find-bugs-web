import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/lib/api'

export type MR = Schemas['MRView']
export type MRState = 'opened' | 'all'

export function mrKey(mr: MR) {
  return `${mr.projectId}!${mr.iid}`
}

function mrPath(mr: MR) {
  return { path: { project: String(mr.projectId), iid: String(mr.iid) } }
}

export function useMRs(state: MRState) {
  return useQuery({
    queryKey: ['mrs', state],
    queryFn: async () => unwrap(await api.GET('/api/mrs', { params: { query: state === 'all' ? { state } : {} } })),
    refetchInterval: 5 * 60_000,
  })
}

export function useMRComments(mr: MR, enabled: boolean) {
  return useQuery({
    queryKey: ['mrs', 'comments', mrKey(mr)],
    queryFn: async () => unwrap(await api.GET('/api/mrs/{project}/{iid}/comments', { params: mrPath(mr) })),
    enabled,
  })
}

export function useMRConflicts(mr: MR, enabled: boolean) {
  return useQuery({
    queryKey: ['mrs', 'conflicts', mrKey(mr)],
    queryFn: async () => unwrap(await api.GET('/api/mrs/{project}/{iid}/conflicts', { params: mrPath(mr) })),
    enabled,
    retry: false,
  })
}

/** Merges one MR; the caller refetches the list when done. */
export function useMergeMR() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (mr: MR) => unwrap(await api.POST('/api/mrs/{project}/{iid}/merge', { params: mrPath(mr) })),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['audit'] }),
  })
}
