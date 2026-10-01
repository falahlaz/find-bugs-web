import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { api, ApiError, setCsrfToken, unwrap } from '@/lib/api'
import { AuthContext, type AuthState } from './auth-context'


export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const me = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      try {
        const s = unwrap(await api.GET('/api/me'))
        setCsrfToken(s.csrfToken)
        return s.user
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null
        throw e
      }
    },
    staleTime: 5 * 60_000,
  })

  const value: AuthState = {
    user: me.data ?? null,
    loading: me.isPending,
    async login(username, password) {
      const s = unwrap(await api.POST('/api/auth/login', { body: { username, password } }))
      setCsrfToken(s.csrfToken)
      qc.setQueryData(['me'], s.user)
    },
    async logout() {
      await api.POST('/api/auth/logout')
      setCsrfToken('')
      qc.clear()
      qc.setQueryData(['me'], null)
    },
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
