import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/lib/api'

export type Repo = Schemas['RepoView']
export type RepoClone = Schemas['RepoCloneView']

export function useRepos() {
  return useQuery({
    queryKey: ['repos'],
    queryFn: async () => unwrap(await api.GET('/api/repos')),
    // Poll faster while a clone runs so the new repo shows up soon.
    refetchInterval: (q) => (q.state.data?.clones.some((c) => c.state === 'cloning') ? 3_000 : 15_000),
  })
}

function useRepoMutation<T>(fn: (project: string) => Promise<T>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['repos'] })
      void qc.invalidateQueries({ queryKey: ['audit'] })
    },
  })
}

export function useCloneRepo() {
  return useRepoMutation(async (project) => unwrap(await api.POST('/api/repos', { body: { project } })))
}

export type Engine = 'claude' | 'agy'

export function useStartSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ project, engine }: { project: string; engine: Engine }) =>
      unwrap(await api.POST('/api/repos/session/start', { body: { project, engine } })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['repos'] })
      void qc.invalidateQueries({ queryKey: ['audit'] })
    },
  })
}

export function useStopSession() {
  return useRepoMutation(async (project) => unwrap(await api.POST('/api/repos/session/stop', { body: { project } })))
}
