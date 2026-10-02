import { Navigate, Outlet, useLocation } from 'react-router'
import { useAuth } from './auth-context'
import { CommandPalette } from './command-palette'
import { useDesign } from './design'
import { BentoShell } from './shell/bento-shell'
import { CommandShell } from './shell/command-shell'
import { TriageShell } from './shell/triage-shell'

const shells = { command: CommandShell, triage: TriageShell, bento: BentoShell }

export function Layout() {
  const { user, loading } = useAuth()
  const { design } = useDesign()
  const location = useLocation()
  if (loading) return <div className="p-6 text-muted-foreground">Memuat…</div>
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />

  const Shell = shells[design]
  return (
    <>
      <Shell>
        <Outlet />
      </Shell>
      <CommandPalette />
    </>
  )
}
