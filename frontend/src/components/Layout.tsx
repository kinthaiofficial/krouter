import { Outlet, NavLink } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { LayoutDashboard, ScrollText, Cpu, Settings2, Bell, Info } from 'lucide-react'
import { api } from '../api/client'

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/logs', label: 'Logs', icon: ScrollText },
  { to: '/providers', label: 'Providers', icon: Cpu },
  { to: '/settings', label: 'Settings', icon: Settings2 },
  { to: '/announcements', label: 'Announcements', icon: Bell },
  { to: '/about', label: 'About', icon: Info },
]

export default function Layout() {
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })
  const { data: annCount } = useQuery({
    queryKey: ['announcements', 'count'],
    queryFn: () => fetch('/internal/announcements/count', { credentials: 'include' })
      .then((r) => r.json()) as Promise<{ unread: number }>,
    refetchInterval: 60_000,
  })

  return (
    <div className="flex min-h-screen bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100">
      {/* Sidebar */}
      <aside className="w-56 shrink-0 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 flex flex-col">
        <div className="px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <span className="font-semibold text-base">krouter</span>
          {status && (
            <span className="ml-2 text-xs text-gray-400">v{status.version}</span>
          )}
        </div>
        <nav className="flex-1 px-3 py-3 space-y-0.5">
          {nav.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                [
                  'flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors',
                  isActive
                    ? 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300 font-medium'
                    : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700',
                ].join(' ')
              }
            >
              <Icon size={16} />
              <span>{label}</span>
              {label === 'Announcements' && (annCount?.unread ?? 0) > 0 && (
                <span className="ml-auto text-xs bg-red-500 text-white rounded-full px-1.5 py-0.5 leading-none">
                  {annCount!.unread}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="px-5 py-3 border-t border-gray-200 dark:border-gray-700 text-xs text-gray-400">
          {status ? (
            <span>proxy :{status.proxy_port}</span>
          ) : (
            <span>connecting…</span>
          )}
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
