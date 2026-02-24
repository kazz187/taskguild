import { Link, Outlet, createRootRoute } from '@tanstack/react-router'
import { FolderKanban } from 'lucide-react'
import { SidebarNav } from '@/components/SidebarNav'
import { SidebarConfig } from '@/components/SidebarConfig'
import { SetupScreen } from '@/components/SetupScreen'
import { useConfig } from '@/components/ConfigProvider'

import '../styles.css'

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  const { isConfigured } = useConfig()

  if (!isConfigured) {
    return <SetupScreen />
  }

  return (
    <div className="min-h-screen bg-slate-950 text-gray-200 flex">
      <aside className="w-56 shrink-0 border-r border-slate-800 bg-slate-900 flex flex-col">
        <Link
          to="/"
          className="flex items-center gap-2 px-4 py-4 border-b border-slate-800"
        >
          <FolderKanban className="w-6 h-6 text-cyan-400" />
          <span className="font-bold text-white text-lg">TaskGuild</span>
        </Link>
        <nav className="flex-1 p-3 overflow-y-auto">
          <SidebarNav />
        </nav>
        <div className="p-3 border-t border-slate-800">
          <SidebarConfig />
        </div>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
