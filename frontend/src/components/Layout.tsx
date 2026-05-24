import { Outlet, NavLink } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Settings2, Bell } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import { storeLang } from '../i18n'

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
  { to: '/', key: 'dashboard', end: true },
  { to: '/free-tokens', key: 'free_tokens' },
  { to: '/router', key: 'router' },
  { to: '/agents', key: 'agents' },
  { to: '/logs', key: 'logs' },
  { to: '/providers', key: 'providers' },
  { to: '/budget', key: 'budget' },
  { to: '/about', key: 'about' },
]

export default function Layout() {
  const { t, i18n } = useTranslation()
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })

  function toggleLang() {
    const next = i18n.language === 'zh' ? 'en' : 'zh'
    i18n.changeLanguage(next)
    storeLang(next)
  }

  const { data: annCount } = useQuery({
    queryKey: ['announcements', 'count'],
    queryFn: () => fetch('/internal/announcements/count', { credentials: 'include' })
      .then((r) => r.json()) as Promise<{ unread: number }>,
    refetchInterval: 60_000,
  })

  const unread = annCount?.unread ?? 0

  return (
    <div className="min-h-screen bg-surface text-gray-900">
      {/* Top nav */}
      <header className="sticky top-0 z-10 bg-white border-b border-border">
        <div className="max-w-7xl mx-auto px-4 py-2 flex items-center gap-2 flex-wrap">
          {/* Brand + version */}
          <div className="flex items-center gap-2 shrink-0 mr-2">
            <img src="/krouter/favicon.svg" alt="" className="w-6 h-6 shrink-0" />
            <span className="font-bold text-sm text-gray-900">KRouter</span>
            {status && (
              <span className="text-xs text-gray-400">{status.version}</span>
            )}
          </div>

          {/* Main nav — text only, no icons; wraps on narrow viewports. */}
          <nav className="flex items-center gap-0.5 flex-wrap flex-1">
            {navItems.map(({ to, key, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  [
                    'px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-brand-light text-brand'
                      : 'text-gray-500 hover:bg-surface hover:text-gray-900',
                  ].join(' ')
                }
              >
                {t(`nav.${key}`)}
              </NavLink>
            ))}
          </nav>

          {/* Right-side icon cluster: lang toggle · notifications · settings */}
          <div className="flex items-center gap-1 shrink-0 ml-auto">
            {/* Language toggle */}
            <button
              type="button"
              onClick={toggleLang}
              className="text-xs font-medium text-gray-500 hover:text-gray-900 px-2 py-1 rounded-md hover:bg-surface transition-colors"
              title={i18n.language === 'zh' ? 'Switch to English' : '切换为中文'}
            >
              {i18n.language === 'zh' ? 'EN' : '中'}
            </button>

            {/* Notifications bell — red dot when unread > 0 */}
            <NavLink
              to="/announcements"
              className={({ isActive }) =>
                [
                  'relative p-1.5 rounded-md transition-colors',
                  isActive
                    ? 'bg-brand-light text-brand'
                    : 'text-gray-500 hover:bg-surface hover:text-gray-900',
                ].join(' ')
              }
              title={t('nav.announcements')}
            >
              <Bell size={16} />
              {unread > 0 && (
                <span className="absolute top-0.5 right-0.5 w-2 h-2 bg-red-500 rounded-full" />
              )}
            </NavLink>

            {/* Settings gear */}
            <NavLink
              to="/settings"
              className={({ isActive }) =>
                [
                  'p-1.5 rounded-md transition-colors',
                  isActive
                    ? 'bg-brand-light text-brand'
                    : 'text-gray-500 hover:bg-surface hover:text-gray-900',
                ].join(' ')
              }
              title={t('nav.settings')}
            >
              <Settings2 size={16} />
            </NavLink>
          </div>
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
