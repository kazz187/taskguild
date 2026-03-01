import { useState, useEffect } from 'react'
import { Link, Outlet, createRootRoute, useLocation } from '@tanstack/react-router'
import { FolderKanban, Menu, X } from 'lucide-react'
import { SidebarNav } from '@/components/SidebarNav'
import { SidebarConfig } from '@/components/SidebarConfig'
import { PushNotificationToggle } from '@/components/PushNotificationToggle'
import { SetupScreen } from '@/components/SetupScreen'
import { useConfig } from '@/components/ConfigProvider'

import '../styles.css'

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  const { isConfigured } = useConfig()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const location = useLocation()

  // Close sidebar when navigating on mobile
  useEffect(() => {
    setSidebarOpen(false)
  }, [location.pathname])

  if (!isConfigured) {
    return <SetupScreen />
  }

  return (
    <div className="h-screen bg-slate-950 text-gray-200 flex">
      {/* Mobile header bar */}
      <div className="fixed top-0 left-0 right-0 z-40 md:hidden bg-slate-900 border-b border-slate-800 flex items-center px-4 py-3">
        <button
          onClick={() => setSidebarOpen(true)}
          className="p-1 text-gray-400 hover:text-white transition-colors"
          aria-label="Open menu"
        >
          <Menu className="w-6 h-6" />
        </button>
        <Link
          to="/"
          className="flex items-center gap-2 ml-3"
        >
          <FolderKanban className="w-5 h-5 text-cyan-400" />
          <span className="font-bold text-white text-lg">TaskGuild</span>
        </Link>
      </div>

      {/* Mobile sidebar overlay */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-50 md:hidden"
          onClick={() => setSidebarOpen(false)}
        >
          {/* Backdrop */}
          <div className="absolute inset-0 bg-black/60" />
          {/* Sidebar panel */}
          <aside
            className="absolute top-0 left-0 bottom-0 w-72 bg-slate-900 flex flex-col shadow-2xl animate-slide-in"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between px-4 py-4 border-b border-slate-800">
              <Link
                to="/"
                className="flex items-center gap-2"
                onClick={() => setSidebarOpen(false)}
              >
                <FolderKanban className="w-6 h-6 text-cyan-400" />
                <span className="font-bold text-white text-lg">TaskGuild</span>
              </Link>
              <button
                onClick={() => setSidebarOpen(false)}
                className="p-1 text-gray-400 hover:text-white transition-colors"
                aria-label="Close menu"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <nav className="flex-1 p-3 overflow-y-auto">
              <SidebarNav />
            </nav>
            <div className="p-3 border-t border-slate-800 space-y-1">
              <PushNotificationToggle />
              <SidebarConfig />
            </div>
          </aside>
        </div>
      )}

      {/* Desktop sidebar (hidden on mobile) */}
      <aside className="hidden md:flex w-56 shrink-0 border-r border-slate-800 bg-slate-900 flex-col">
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
        <div className="p-3 border-t border-slate-800 space-y-1">
          <PushNotificationToggle />
          <SidebarConfig />
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto pt-14 md:pt-0">
        <Outlet />
      </main>
    </div>
  )
}
