import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '@/lib/api'
import { isFinal, type JobStatus } from './status'

export function useEnvironments() {
  return useQuery({
    queryKey: ['environments'],
    queryFn: async () => unwrap(await api.GET('/api/environments')),
    staleTime: Infinity,
  })
}

/** Timezone used to display times (falls back until /api/environments loads). */
export function useTimezone() {
  return useEnvironments().data?.timezone ?? 'Asia/Jakarta'
}

export function useJob(id: number) {
  return useQuery({
    queryKey: ['job', id],
    queryFn: async () => unwrap(await api.GET('/api/jobs/{id}', { params: { path: { id } } })),
    // Poll while the job is still moving; stop once it is final.
    refetchInterval: (q) => (q.state.data && isFinal(q.state.data.status as JobStatus) ? false : 2500),
  })
}

export type JobFilters = {
  environment?: string
  status?: string
  transactionId?: string
  from?: string
  to?: string
  mine?: '1'
}

export const PAGE_SIZE = 50

export function useJobs(filters: JobFilters, beforeId?: number) {
  return useQuery({
    queryKey: ['jobs', filters, beforeId],
    queryFn: async () => {
      const query: Record<string, string> = { limit: String(PAGE_SIZE) }
      for (const [k, v] of Object.entries(filters)) if (v) query[k] = v
      if (beforeId) query.beforeId = String(beforeId)
      return unwrap(await api.GET('/api/jobs', { params: { query } })).jobs
    },
    refetchInterval: 5000,
  })
}

export function useSubmitJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { environment: string; timeRange: '24h' | '48h'; input: string; force?: boolean }) =>
      unwrap(await api.POST('/api/jobs', { body })),
    onSuccess: (res) => {
      qc.setQueryData(['job', res.job.id], res.job)
      void qc.invalidateQueries({ queryKey: ['jobs'] })
      void qc.invalidateQueries({ queryKey: ['system-status'] })
    },
  })
}

export function useCancelJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => unwrap(await api.POST('/api/jobs/{id}/cancel', { params: { path: { id } } })),
    onSuccess: (job) => {
      qc.setQueryData(['job', job.id], job)
      void qc.invalidateQueries({ queryKey: ['jobs'] })
    },
  })
}
