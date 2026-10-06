import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/lib/api'

export type Tool = Schemas['ToolInfo']
export type ToolField = Schemas['ToolField']
export type ToolResult = Schemas['ToolResult']
export type ToolValues = Record<string, string | boolean>

export function useTools() {
  return useQuery({
    queryKey: ['tools'],
    queryFn: async () => unwrap(await api.GET('/api/tools')).tools,
    staleTime: Infinity,
  })
}

export function useRunTool(tool: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: ToolValues) => unwrap(await api.POST('/api/tools/{tool}/run', { params: { path: { tool } }, body })),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['audit'] }),
  })
}
