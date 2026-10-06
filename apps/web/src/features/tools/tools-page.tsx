import { Cookie, FileKey2, KeyRound, Link2, Wrench, type LucideIcon } from 'lucide-react'
import { Navigate, NavLink, useParams } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { cn } from '@/lib/utils'
import { useRunTool, useTools, type Tool } from './queries'
import { ToolForm } from './tool-form'
import { ToolResultView } from './tool-result'

const icons: Record<string, LucideIcon> = {
  'payment-deeplink': Link2,
  hashsign: FileKey2,
  'package-id': KeyRound,
  'token-decrypt': Cookie,
}

function ToolPane({ tool }: { tool: Tool }) {
  const run = useRunTool(tool.id)
  return (
    <div className="grid items-start gap-4 @4xl:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>{tool.name}</CardTitle>
          <CardDescription>
            {tool.description}
            {tool.docs && <span className="mt-1 block text-xs">{tool.docs}</span>}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ToolForm key={tool.id} tool={tool} pending={run.isPending} onRun={(v) => run.mutate(v)} />
        </CardContent>
      </Card>
      <Card aria-live="polite">
        <CardHeader>
          <CardTitle>Hasil</CardTitle>
        </CardHeader>
        <CardContent>
          {run.error && (
            <Alert tone="danger">
              <span className="whitespace-pre-wrap">{run.error.message}</span>
            </Alert>
          )}
          {run.data && !run.error && <ToolResultView result={run.data} />}
          {!run.data && !run.error && <p className="text-sm text-muted-foreground">Isi form lalu jalankan; hasilnya muncul di sini.</p>}
        </CardContent>
      </Card>
    </div>
  )
}

/** The MyTelkomsel support tools (ported from mytsel-tools). */
export function ToolsPage() {
  const { id } = useParams()
  const tools = useTools()
  const list = tools.data ?? []
  const tool = list.find((t) => t.id === id)

  if (list.length > 0 && !tool) return <Navigate to={`/tools/${list[0].id}`} replace />

  return (
    <div className="grid gap-4">
      <PageHeader title="Tools" description="Deeplink, hashsign, package ID dan token decrypt MyTelkomsel." />
      {tools.error && <Alert tone="danger">{tools.error.message}</Alert>}
      <nav aria-label="Tools" className="-mx-4 flex gap-1.5 overflow-x-auto px-4 pb-1 md:mx-0 md:flex-wrap md:px-0">
        {list.map((t) => {
          const Icon = icons[t.id] ?? Wrench
          return (
            <NavLink
              key={t.id}
              to={`/tools/${t.id}`}
              className={({ isActive }) =>
                cn(
                  'inline-flex shrink-0 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground',
                  isActive && 'border-primary bg-primary text-primary-foreground hover:text-primary-foreground',
                )
              }
            >
              <Icon className="size-4" aria-hidden />
              {t.name}
            </NavLink>
          )
        })}
      </nav>
      {tool && <ToolPane key={tool.id} tool={tool} />}
    </div>
  )
}
