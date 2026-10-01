import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { api, unwrap } from '@/lib/api'
import { requestNotificationPermission } from '@/lib/notify'
import type { Job } from './status'

/** Re-submits a job with the same parameters, skipping the duplicate check. */
export function useRerun() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: async (job: Job) => {
      requestNotificationPermission()
      return unwrap(
        await api.POST('/api/jobs', {
          body: { environment: job.environment, timeRange: job.timeRange as '24h' | '48h', input: job.transactionId, force: true },
        }),
      )
    },
    onSuccess: (res) => {
      qc.setQueryData(['job', res.job.id], res.job)
      void qc.invalidateQueries({ queryKey: ['jobs'] })
      navigate(`/jobs/${res.job.id}`)
    },
  })
}
