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
      // qc.clear() would also drop the ['me'] query that AuthProvider's
      // observer is attached to; the null written afterwards lands on a new
      // query nobody watches, so Layout never re-renders to redirect.
      qc.setQueryData(['me'], null)
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'me' })
    },
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
