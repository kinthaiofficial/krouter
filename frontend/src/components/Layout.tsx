import { useEffect, useState } from 'react'
import { Outlet, NavLink } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  LayoutDashboard,
  Route as RouteIcon,
  Bot,
  ScrollText,
  Cpu,
  Settings2,
  Bell,
  Info,
  PanelLeftClose,
  PanelLeftOpen,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

const navItems = [
  { to: '/', key: 'dashboard', icon: LayoutDashboard, end: true },
  { to: '/router', key: 'router', icon: RouteIcon },
  { to: '/agents', key: 'agents', icon: Bot },
  { to: '/logs', key: 'logs', icon: ScrollText },
  { to: '/providers', key: 'providers', icon: Cpu },
  { to: '/settings', key: 'settings', icon: Settings2 },
  { to: '/announcements', key: 'announcements', icon: Bell },
  { to: '/about', key: 'about', icon: Info },
]

const COLLAPSED_KEY = 'krouter:sidebar-collapsed'

export default function Layout() {
  const { t } = useTranslation()
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })
  const { data: annCount } = useQuery({
    queryKey: ['announcements', 'count'],
    queryFn: () => fetch('/internal/announcements/count', { credentials: 'include' })
      .then((r) => r.json()) as Promise<{ unread: number }>,
    refetchInterval: 60_000,
  })

  // Persist collapsed preference in localStorage so it survives reloads.
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem(COLLAPSED_KEY) === '1' } catch { return false }
  })
  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSED_KEY, collapsed ? '1' : '0')
    } catch (e) {
      // Safari Private Browsing throws SecurityError on localStorage.setItem;
      // warn so the user/operator sees why the preference isn't persisting
      // instead of silently dropping it.
      console.warn('krouter: failed to persist sidebar state:', e)
    }
  }, [collapsed])

  return (
    <div className="flex min-h-screen bg-surface text-gray-900">
      {/* Sidebar */}
      <aside
        className={[
          'shrink-0 bg-white border-r border-border flex flex-col transition-all duration-200',
          collapsed ? 'w-14' : 'w-56',
        ].join(' ')}
      >
        <div
          className={[
            'border-b border-border flex items-center gap-3',
            collapsed ? 'px-3 py-4 justify-center' : 'px-5 py-4',
          ].join(' ')}
        >
          <img src="/krouter/favicon.svg" alt="" className="w-7 h-7 shrink-0" />
          {!collapsed && (
            <div className="flex-1 min-w-0">
              <span className="font-bold text-sm text-gray-900">KRouter</span>
              {status && (
                <span className="ml-1.5 text-xs text-gray-400">{status.version}</span>
              )}
            </div>
          )}
        </div>
        <nav className={['flex-1 py-3 space-y-0.5', collapsed ? 'px-2' : 'px-3'].join(' ')}>
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
                title={collapsed ? t(`nav.${key}`) : undefined}
                className={({ isActive }) =>
                  [
                    'flex items-center rounded-lg text-sm font-medium transition-colors relative',
                    collapsed ? 'justify-center px-2 py-2' : 'gap-2.5 px-3 py-2',
                    isActive
                      ? 'bg-brand-light text-brand'
                      : 'text-gray-500 hover:bg-surface hover:text-gray-900',
                  ].join(' ')
                }
              >
                <Icon size={16} />
                {!collapsed && <span>{t(`nav.${key}`)}</span>}
                {badge !== null && !collapsed && (
                  <span className="ml-auto text-xs bg-red-500 text-white rounded-full px-1.5 py-0.5 leading-none">
                    {badge}
                  </span>
                )}
                {badge !== null && collapsed && (
                  <span
                    aria-label={`${badge} unread`}
                    className="absolute top-1 right-1 w-2 h-2 bg-red-500 rounded-full"
                  />
                )}
              </NavLink>
            )
          })}
        </nav>
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          title={collapsed ? t('nav.expand') : t('nav.collapse')}
          aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
          aria-pressed={collapsed}
          className={[
            'mx-2 mb-2 mt-1 flex items-center gap-2 px-2 py-2 rounded-lg text-xs text-gray-400 hover:bg-surface hover:text-gray-700 transition-colors',
            collapsed ? 'justify-center' : '',
          ].join(' ')}
        >
          {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
          {!collapsed && <span>{t('nav.collapse')}</span>}
        </button>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
