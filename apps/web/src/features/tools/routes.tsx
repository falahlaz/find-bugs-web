import { MenuGuard } from '@/app/menu-guard'
import { ToolsPage } from './tools-page'

export const toolsRoutes = [
  { path: 'tools', element: <MenuGuard menu="tools"><ToolsPage /></MenuGuard> },
  { path: 'tools/:id', element: <MenuGuard menu="tools"><ToolsPage /></MenuGuard> },
]
