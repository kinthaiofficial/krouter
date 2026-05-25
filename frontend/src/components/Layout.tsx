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
    <div className="min-h-screen bg-surface text-ink">
      {/* Top nav */}
      <header className="sticky top-0 z-10 bg-surface/85 backdrop-blur border-b border-line">
        <div className="max-w-6xl mx-auto px-6 h-14 flex items-center gap-3 flex-wrap">
          {/* Brand */}
          <div className="flex items-center gap-2 shrink-0">
            <img src="/krouter/favicon.svg" alt="" className="w-6 h-6 shrink-0 rounded-md" />
            <span className="font-bold text-[15px] tracking-tight text-ink">KRouter</span>
          </div>

          {/* LIVE indicator — daemon reachable */}
          {status && (
            <span className="flex items-center gap-1.5 text-xs font-semibold text-brand-ink shrink-0">
              <span className="w-[7px] h-[7px] rounded-full bg-brand shadow-[0_0_0_3px_var(--color-brand-soft)]" />
              LIVE
            </span>
          )}

          {/* Main nav — text only, no icons; wraps on narrow viewports. */}
          <nav className="flex items-center gap-px flex-wrap flex-1 ml-1">
            {navItems.map(({ to, key, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  [
                    'px-3 py-1.5 rounded-md text-[13px] transition-colors',
                    isActive
                      ? 'bg-gray-100 text-ink font-semibold'
                      : 'text-muted hover:bg-gray-100 hover:text-ink font-medium',
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
              className="w-[30px] h-[30px] grid place-items-center text-xs font-semibold text-muted hover:text-ink border border-line-strong rounded-lg bg-card transition-colors"
              title={i18n.language === 'zh' ? 'Switch to English' : '切换为中文'}
            >
              {i18n.language === 'zh' ? 'EN' : '中'}
            </button>

            {/* Notifications bell — red dot when unread > 0 */}
            <NavLink
              to="/announcements"
              className={({ isActive }) =>
                [
                  'relative w-[30px] h-[30px] grid place-items-center rounded-lg border transition-colors',
                  isActive
                    ? 'bg-brand-soft text-brand-ink border-brand-soft'
                    : 'text-muted hover:text-ink bg-card border-line-strong',
                ].join(' ')
              }
              title={t('nav.announcements')}
            >
              <Bell size={15} />
              {unread > 0 && (
                <span className="absolute top-1 right-1 w-[7px] h-[7px] bg-red-500 rounded-full ring-2 ring-card" />
              )}
            </NavLink>

            {/* Settings gear */}
            <NavLink
              to="/settings"
              className={({ isActive }) =>
                [
                  'w-[30px] h-[30px] grid place-items-center rounded-lg border transition-colors',
                  isActive
                    ? 'bg-brand-soft text-brand-ink border-brand-soft'
                    : 'text-muted hover:text-ink bg-card border-line-strong',
                ].join(' ')
              }
              title={t('nav.settings')}
            >
              <Settings2 size={15} />
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
