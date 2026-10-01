import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '@/lib/api'

export function useSplunkStatus() {
  return useQuery({
    queryKey: ['splunk', 'status'],
    queryFn: async () => unwrap(await api.GET('/api/splunk/status')),
    // Poll fast while a re-auth waits for the 2FA push.
    refetchInterval: (q) => (q.state.data?.reauthing || q.state.data?.session.reauthRunning ? 2000 : 10_000),
  })
}

export function useSplunkReauth() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/api/splunk/reauth')),
    onSuccess: (data) => qc.setQueryData(['splunk', 'status'], data),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['system-status'] }),
  })
}
