import { Outlet, NavLink } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  LayoutDashboard,
  Gift,
  Route as RouteIcon,
  Bot,
  ScrollText,
  Cpu,
  Wallet,
  Settings2,
  Bell,
  Info,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

// Layout was originally a left sidebar but the narrower viewport sizes
// (≤ 1280 px) left the content column too cramped on dense pages like
// Router or Providers. Moving the nav to a single top bar reclaims that
// horizontal real estate — pages with `max-w-*xl mx-auto` continue to
// centre themselves but no longer compete with a 224 px aside.
//
// On narrower viewports the nav row wraps via `flex-wrap`; we don't
// bother with a hamburger menu because the dashboard is laptop / desktop
// territory in practice.

const navItems = [
  { to: '/', key: 'dashboard', icon: LayoutDashboard, end: true },
  { to: '/free-tokens', key: 'free_tokens', icon: Gift },
  { to: '/router', key: 'router', icon: RouteIcon },
  { to: '/agents', key: 'agents', icon: Bot },
  { to: '/logs', key: 'logs', icon: ScrollText },
  { to: '/providers', key: 'providers', icon: Cpu },
  { to: '/budget', key: 'budget', icon: Wallet },
  { to: '/settings', key: 'settings', icon: Settings2 },
  { to: '/announcements', key: 'announcements', icon: Bell },
  { to: '/about', key: 'about', icon: Info },
]

export default function Layout() {
  const { t } = useTranslation()
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })
  const { data: annCount } = useQuery({
    queryKey: ['announcements', 'count'],
    queryFn: () => fetch('/internal/announcements/count', { credentials: 'include' })
      .then((r) => r.json()) as Promise<{ unread: number }>,
    refetchInterval: 60_000,
  })

  return (
    <div className="min-h-screen bg-surface text-gray-900">
      {/* Top nav */}
      <header className="sticky top-0 z-10 bg-white border-b border-border">
        <div className="max-w-7xl mx-auto px-4 py-2 flex items-center gap-3 flex-wrap">
          {/* Brand + version */}
          <div className="flex items-center gap-2 shrink-0 mr-2">
            <img src="/krouter/favicon.svg" alt="" className="w-6 h-6 shrink-0" />
            <span className="font-bold text-sm text-gray-900">KRouter</span>
            {status && (
              <span className="text-xs text-gray-400">{status.version}</span>
            )}
          </div>

          {/* Nav items — wrap to a second row on narrow viewports. */}
          <nav className="flex items-center gap-0.5 flex-wrap">
            {navItems.map(({ to, key, icon: Icon, end }) => {
              const badge =
                key === 'announcements' && (annCount?.unread ?? 0) > 0
                  ? annCount!.unread
                  : null
              return (
                <NavLink
                  key={to}
                  to={to}
                  end={end}
                  className={({ isActive }) =>
                    [
                      'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors relative',
                      isActive
                        ? 'bg-brand-light text-brand'
                        : 'text-gray-500 hover:bg-surface hover:text-gray-900',
                    ].join(' ')
                  }
                >
                  <Icon size={15} />
                  <span>{t(`nav.${key}`)}</span>
                  {badge !== null && (
                    <span className="text-[10px] bg-red-500 text-white rounded-full px-1.5 py-0.5 leading-none">
                      {badge}
                    </span>
                  )}
                </NavLink>
              )
            })}
          </nav>
        </div>
      </header>

      {/* Main content — no longer compressed by a sidebar. Pages still set
          their own max-w-*xl mx-auto, so widescreens centre, narrow
          viewports fill. */}
      <main>
        <Outlet />
      </main>
    </div>
  )
}
