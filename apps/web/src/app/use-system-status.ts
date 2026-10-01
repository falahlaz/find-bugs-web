import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '@/lib/api'

/** Polls the shared VPN/Splunk/queue state that drives the global banners. */
export function useSystemStatus() {
  return useQuery({
    queryKey: ['system-status'],
    queryFn: async () => unwrap(await api.GET('/api/system/status')),
    refetchInterval: 3000,
  })
}
