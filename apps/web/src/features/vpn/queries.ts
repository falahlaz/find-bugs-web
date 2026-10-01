import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '@/lib/api'

const busyStates = ['CONNECTING', 'WAITING_CALLBACK', 'SUBMITTING']

/** Live status (runs `globalprotect show --status` on the server). */
export function useVpnStatus() {
  return useQuery({
    queryKey: ['vpn', 'status'],
    queryFn: async () => unwrap(await api.GET('/api/vpn/status')),
    refetchInterval: (q) => (q.state.data && busyStates.includes(q.state.data.state) ? 2000 : 10_000),
  })
}

/** Polls for the captured SAML link while a connect attempt is running. */
export function useLoginUrl(enabled: boolean) {
  return useQuery({
    queryKey: ['vpn', 'login-url'],
    queryFn: async () => unwrap(await api.GET('/api/vpn/login-url')),
    enabled,
    refetchInterval: (q) => (q.state.data?.url ? false : 1500),
  })
}

export function useVpnLogs(enabled: boolean) {
  return useQuery({
    queryKey: ['vpn', 'logs'],
    queryFn: async () => unwrap(await api.GET('/api/vpn/logs')).lines,
    enabled,
    refetchInterval: 5000,
  })
}

function useInvalidateVpn() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['vpn'] })
    void qc.invalidateQueries({ queryKey: ['system-status'] })
  }
}

export function useVpnConnect() {
  const invalidate = useInvalidateVpn()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/api/vpn/connect')),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ['vpn', 'login-url'] })
      invalidate()
    },
  })
}

export function useVpnCallback() {
  const invalidate = useInvalidateVpn()
  return useMutation({
    mutationFn: async (uri: string) => unwrap(await api.POST('/api/vpn/callback', { body: { uri } })),
    onSettled: invalidate,
  })
}

export function useVpnDisconnect() {
  const invalidate = useInvalidateVpn()
  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/api/vpn/disconnect')),
    onSettled: invalidate,
  })
}
