import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Menu } from '@/app/menus'
import { api, unwrap, type Schemas } from '@/lib/api'

export type Role = Schemas['User']['role']

export function useUsers() {
  return useQuery({ queryKey: ['users'], queryFn: async () => unwrap(await api.GET('/api/users')).users })
}

export function useCreateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { username: string; password: string; role: Role; menus?: Menu[] }) => unwrap(await api.POST('/api/users', { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['users'] }),
  })
}

export function useUpdateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...body }: { id: number; role?: Role; active?: boolean; password?: string; menus?: Menu[] }) =>
      unwrap(await api.PATCH('/api/users/{id}', { params: { path: { id } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['users'] })
      void qc.invalidateQueries({ queryKey: ['audit'] })
    },
  })
}

export function useAudit() {
  return useQuery({
    queryKey: ['audit'],
    queryFn: async () => unwrap(await api.GET('/api/audit', { params: { query: { limit: '200' } } })).entries,
    refetchInterval: 15_000,
  })
}
