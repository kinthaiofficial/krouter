import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import { useTranslation } from 'react-i18next'
import { api, type LogRecord, type Preset, type DashboardStats } from '../api/client'
import PresetSwitcher from '../components/PresetSwitcher'
import QuotaBar from '../components/QuotaBar'
import SubscriptionQuotaCard from '../components/SubscriptionQuotaCard'
import FreeProvidersCard from '../components/FreeProvidersCard'

const PROVIDER_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#ef4444', '#06b6d4']

export default function Dashboard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: budget } = useQuery({ queryKey: ['budget'], queryFn: api.budget })
  const { data: quotas } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: logsData } = useQuery({ queryKey: ['logs'], queryFn: () => api.logs(20) })
  const { data: presetData } = useQuery({ queryKey: ['preset'], queryFn: api.preset })
  const { data: dashStats } = useQuery<DashboardStats>({
    queryKey: ['dashboard-stats'],
    queryFn: api.dashboardStats,
    refetchInterval: 30_000,
  })

  const [recentLogs, setRecentLogs] = useState<LogRecord[]>([])
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    if (logsData) setRecentLogs(logsData)
  }, [logsData])

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    esRef.current = es
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        setRecentLogs((prev) => [rec, ...prev].slice(0, 50))
        qc.invalidateQueries({ queryKey: ['budget'] })
        qc.invalidateQueries({ queryKey: ['quota'] })
      } catch { /* ignore */ }
    })
    es.addEventListener('settings_changed', () => {
      qc.invalidateQueries({ queryKey: ['preset'] })
      qc.invalidateQueries({ queryKey: ['settings'] })
    })
    es.addEventListener('budget_warning', () => {
      qc.invalidateQueries({ queryKey: ['budget'] })
    })
    return () => es.close()
  }, [qc])

  const setPreset = useMutation({
    mutationFn: (p: Preset) => api.setPreset(p),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['preset'] }),
  })

  return (
    <div className="p-6 space-y-5 max-w-5xl mx-auto">
      <h1 className="text-lg font-semibold">{t('dashboard.title')}</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <PresetSwitcher
          current={presetData?.preset ?? 'balanced'}
          onSelect={(p) => setPreset.mutate(p)}
        />

        {/* Today stats */}
        <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-2">
          <h2 className="text-sm font-medium text-gray-500">{t('dashboard.today')}</h2>
          <div className="flex gap-6">
            <Stat label={t('dashboard.requests')} value={String(budget?.requests_today ?? 0)} />
            <Stat label={t('dashboard.saved')} value={`$${(budget?.savings_today_usd ?? 0).toFixed(3)}`} />
            <Stat label={t('dashboard.spent')} value={`$${(budget?.cost_today_usd ?? 0).toFixed(3)}`} />
          </div>
        </div>
      </div>

      {budget?.daily_limit_usd && (
        <BudgetBar
          costUSD={budget.cost_today_usd}
          limitUSD={budget.daily_limit_usd}
          pct={budget.daily_percent_used ?? 0}
          blocked={budget.budget_blocked ?? false}
        />
      )}

      {dashStats && (
        <section className="bg-white rounded-xl border border-gray-200 p-5">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-sm font-medium text-gray-700">{t('dashboard.this_week')}</h2>
            <span className="text-xs text-gray-400">
              {t('dashboard.agents_connected', { count: dashStats.agents_connected })}
            </span>
          </div>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <p className="text-2xl font-bold tabular-nums">{dashStats.weekly.requests.toLocaleString()}</p>
              <p className="text-xs text-gray-400 mt-0.5">{t('dashboard.requests_label')}</p>
            </div>
            <div>
              <p className="text-2xl font-bold tabular-nums">${dashStats.weekly.cost_usd.toFixed(3)}</p>
              <p className="text-xs text-gray-400 mt-0.5">{t('dashboard.spent_label')}</p>
            </div>
            <div>
              <p className="text-2xl font-bold tabular-nums text-green-600">${dashStats.weekly.savings_usd.toFixed(3)}</p>
              <p className="text-xs text-gray-400 mt-0.5">{t('dashboard.saved_label')}</p>
            </div>
          </div>
        </section>
      )}

      {dashStats && dashStats.providers.length > 0 && (
        <section className="bg-white rounded-xl border border-gray-200 p-5">
          <h2 className="text-sm font-medium text-gray-700 mb-4">{t('dashboard.provider_distribution')}</h2>
          <div className="flex items-center gap-6">
            <div className="w-32 h-32 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={dashStats.providers}
                    dataKey="requests"
                    nameKey="name"
                    cx="50%"
                    cy="50%"
                    innerRadius={28}
                    outerRadius={52}
                  >
                    {dashStats.providers.map((p, i) => (
                      <Cell key={p.name} fill={PROVIDER_COLORS[i % PROVIDER_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    formatter={(value) => [`${String(value)} requests`, '']}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="flex-1 space-y-1.5">
              {dashStats.providers.map((p, i) => (
                <div key={p.name} className="flex items-center gap-2 text-sm">
                  <span
                    className="w-2.5 h-2.5 rounded-full shrink-0"
                    style={{ background: PROVIDER_COLORS[i % PROVIDER_COLORS.length] }}
                  />
                  <span className="flex-1 capitalize">{p.name}</span>
                  <span className="tabular-nums text-gray-500">{p.requests}</span>
                </div>
              ))}
            </div>
          </div>
        </section>
      )}

      {/* Quota bars */}
      {quotas && quotas.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
          <h2 className="text-sm font-medium text-gray-500">Quota</h2>
          {quotas.map((q) => <QuotaBar key={q.window} quota={q} />)}
        </div>
      )}

      {/* Subscription quota — collapses to nothing when no providers polled. */}
      <SubscriptionQuotaCard />

      {/* Free LLM credits — catalog of providers offering free tokens. */}
      <FreeProvidersCard />

      {/* Recent requests */}
      <div className="bg-white rounded-xl border border-gray-200 p-5">
        <h2 className="text-sm font-medium text-gray-500 mb-4">{t('dashboard.recent_requests')}</h2>
        <RequestTable logs={recentLogs} />
      </div>
    </div>
  )
}

function BudgetBar({ costUSD, limitUSD, pct, blocked }: {
  costUSD: number; limitUSD: number; pct: number; blocked: boolean
}) {
  const { t } = useTranslation()
  const clampedPct = Math.min(pct, 1)
  const barColor = blocked
    ? 'bg-red-500'
    : pct >= 0.95 ? 'bg-red-400'
    : pct >= 0.80 ? 'bg-yellow-400'
    : 'bg-green-400'

  return (
    <section className={[
      'rounded-xl border p-4',
      blocked ? 'bg-red-50 border-red-200' : 'bg-white border-gray-200',
    ].join(' ')}>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-medium text-gray-700">{t('dashboard.budget_daily')}</span>
        <span className={['text-sm font-medium', blocked ? 'text-red-600' : 'text-gray-600'].join(' ')}>
          ${costUSD.toFixed(2)} / ${limitUSD.toFixed(0)}
          {blocked && <span className="ml-2 text-xs font-semibold uppercase tracking-wide">{t('dashboard.budget_blocked')}</span>}
        </span>
      </div>
      <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-500 ${barColor}`}
          style={{ width: `${clampedPct * 100}%` }}
        />
      </div>
      {blocked && (
        <p className="text-xs text-red-600 mt-1.5">
          {t('dashboard.budget_unblock_hint')}
        </p>
      )}
    </section>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-2xl font-bold">{value}</p>
      <p className="text-xs text-gray-500">{label}</p>
    </div>
  )
}

function RequestTable({ logs }: { logs: LogRecord[] }) {
  const { t } = useTranslation()
  if (logs.length === 0) return <p className="text-sm text-gray-400">{t('dashboard.no_requests')}</p>
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs text-gray-400 border-b border-gray-100">
            {[
              t('dashboard.col_time'),
              t('dashboard.col_agent'),
              t('dashboard.col_model'),
              t('dashboard.col_provider'),
              t('dashboard.col_cost'),
              t('dashboard.col_latency'),
            ].map((h) => (
              <th key={h} className="pb-2 pr-4 last:text-right">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-50">
          {logs.map((log) => (
            <tr key={log.id} className="hover:bg-gray-50">
              <td className="py-1.5 pr-4 text-gray-400 text-xs whitespace-nowrap">{new Date(log.ts).toLocaleTimeString()}</td>
              <td className="py-1.5 pr-4">{log.agent ?? '—'}</td>
              <td className="py-1.5 pr-4 font-mono text-xs">{log.model}</td>
              <td className="py-1.5 pr-4">{log.provider}</td>
              <td className="py-1.5 pr-4 text-xs">${log.cost_usd.toFixed(4)}</td>
              <td className="py-1.5 text-right text-xs">{log.latency_ms}ms</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
