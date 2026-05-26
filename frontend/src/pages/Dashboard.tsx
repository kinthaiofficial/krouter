import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import { useTranslation } from 'react-i18next'
import { api, type Preset, type DashboardStats } from '../api/client'
import PresetSwitcher from '../components/PresetSwitcher'
import QuotaBar from '../components/QuotaBar'
import { Panel, Badge } from '../components/ui'

const PROVIDER_COLORS = ['#0fa46a', '#3b82f6', '#8b5cf6', '#f59e0b', '#ef4444', '#06b6d4']

export default function Dashboard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })
  const { data: budget } = useQuery({ queryKey: ['budget'], queryFn: api.budget })
  const { data: quotas } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: presetData } = useQuery({ queryKey: ['preset'], queryFn: api.preset })
  const { data: dashStats } = useQuery<DashboardStats>({
    queryKey: ['dashboard-stats'],
    queryFn: api.dashboardStats,
    refetchInterval: 30_000,
  })

  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    esRef.current = es
    es.addEventListener('request_completed', () => {
      qc.invalidateQueries({ queryKey: ['budget'] })
      qc.invalidateQueries({ queryKey: ['quota'] })
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

  const savings = budget?.savings_today_usd ?? 0
  const cost = budget?.cost_today_usd ?? 0
  const savedPct = savings + cost > 0 ? Math.round((savings / (savings + cost)) * 100) : 0

  return (
    <div className="p-6 max-w-6xl mx-auto">
      {/* Head */}
      <div className="flex items-baseline justify-between gap-4 mb-4 flex-wrap">
        <h1 className="text-lg font-bold tracking-tight">{t('dashboard.title')}</h1>
        <span className="text-xs text-faint font-mono tabular-nums">
          {dashStats ? `${dashStats.agents_connected} agents` : ''}
          {status ? ` · :${status.proxy_port}` : ''}
        </span>
      </div>

      {/* KPI strip — today, real numbers only */}
      <div className="grid grid-cols-2 md:grid-cols-4 bg-card border border-line rounded-xl overflow-hidden mb-4">
        <Kpi rail="#767c89" label={t('dashboard.requests')} value={String(budget?.requests_today ?? 0)} />
        <Kpi rail="#0fa46a" label={t('dashboard.saved')} value={`$${savings.toFixed(3)}`} accent />
        <Kpi rail="#3b82f6" label={t('dashboard.spent')} value={`$${cost.toFixed(3)}`} />
        <Kpi rail="#f59e0b" label={t('dashboard.saved_label')} value={`${savedPct}%`} />
      </div>

      {/* Preset + provider distribution */}
      <div className="grid md:grid-cols-[300px_1fr] gap-4 items-start">
        <PresetSwitcher
          current={presetData?.preset ?? 'balanced'}
          onSelect={(p) => setPreset.mutate(p)}
        />

        {dashStats && dashStats.providers.length > 0 ? (
          <Panel
            title={t('dashboard.provider_distribution')}
            right={<Badge>{t('dashboard.agents_connected', { count: dashStats.agents_connected })}</Badge>}
          >
            <ProviderDist providers={dashStats.providers} />
          </Panel>
        ) : (
          <Panel title={t('dashboard.provider_distribution')}>
            <p className="text-sm text-faint">{t('dashboard.no_requests')}</p>
          </Panel>
        )}
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
        <Panel className="mt-4" title={t('dashboard.this_week')}>
          <div className="grid grid-cols-3 divide-x divide-line">
            <WeekStat value={dashStats.weekly.requests.toLocaleString()} label={t('dashboard.requests_label')} />
            <WeekStat value={`$${dashStats.weekly.cost_usd.toFixed(3)}`} label={t('dashboard.spent_label')} />
            <WeekStat value={`$${dashStats.weekly.savings_usd.toFixed(3)}`} label={t('dashboard.saved_label')} accent />
          </div>
        </Panel>
      )}

      {quotas && quotas.length > 0 && (
        <Panel className="mt-4" title={t('dashboard.quota')}>
          <div className="space-y-4">
            {quotas.map((q) => <QuotaBar key={q.window} quota={q} />)}
          </div>
        </Panel>
      )}

    </div>
  )
}

function Kpi({ rail, label, value, accent }: { rail: string; label: string; value: string; accent?: boolean }) {
  return (
    <div className="relative px-4 py-4 border-r border-b border-line last:border-r-0 md:border-b-0 [&:nth-child(2)]:border-r-0 md:[&:nth-child(2)]:border-r [&:nth-child(3)]:border-b-0">
      <span className="absolute left-0 top-3.5 bottom-3.5 w-[3px] rounded-r" style={{ background: rail }} />
      <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">{label}</p>
      <p className={['text-2xl font-bold tabular-nums mt-1.5 font-mono', accent ? 'text-brand-ink' : 'text-ink'].join(' ')}>
        {value}
      </p>
    </div>
  )
}

function ProviderDist({ providers }: { providers: { name: string; requests: number }[] }) {
  const max = Math.max(...providers.map((p) => p.requests), 1)
  return (
    <div className="flex items-center gap-6">
      <div className="w-28 h-28 shrink-0">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie data={providers} dataKey="requests" nameKey="name" cx="50%" cy="50%" innerRadius={26} outerRadius={50}>
              {providers.map((p, i) => (
                <Cell key={p.name} fill={PROVIDER_COLORS[i % PROVIDER_COLORS.length]} />
              ))}
            </Pie>
            <Tooltip formatter={(value) => [`${String(value)} requests`, '']} />
          </PieChart>
        </ResponsiveContainer>
      </div>
      <div className="flex-1 space-y-2.5">
        {providers.map((p, i) => (
          <div key={p.name} className="flex items-center gap-3 text-sm">
            <span className="w-2.5 h-2.5 rounded-sm shrink-0" style={{ background: PROVIDER_COLORS[i % PROVIDER_COLORS.length] }} />
            <span className="flex-1 capitalize font-medium">{p.name}</span>
            <span className="w-24 h-1.5 bg-gray-100 rounded-full overflow-hidden hidden sm:block">
              <span className="block h-full rounded-full" style={{ width: `${(p.requests / max) * 100}%`, background: PROVIDER_COLORS[i % PROVIDER_COLORS.length] }} />
            </span>
            <span className="tabular-nums font-mono text-muted w-12 text-right">{p.requests}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function WeekStat({ value, label, accent }: { value: string; label: string; accent?: boolean }) {
  return (
    <div className="px-4 first:pl-0 last:pr-0">
      <p className={['text-2xl font-bold tabular-nums font-mono', accent ? 'text-brand-ink' : 'text-ink'].join(' ')}>{value}</p>
      <p className="text-xs text-muted mt-1">{label}</p>
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
    : pct >= 0.80 ? 'bg-amber-400'
    : 'bg-brand'

  return (
    <section className={[
      'rounded-xl border mt-4',
      blocked ? 'bg-red-50 border-red-200' : 'bg-card border-line',
    ].join(' ')}>
      <div className="p-4">
        <div className="flex items-center justify-between mb-2.5">
          <span className="text-sm font-medium text-muted">{t('dashboard.budget_daily')}</span>
          <span className={['text-sm font-bold font-mono tabular-nums', blocked ? 'text-red-600' : 'text-ink'].join(' ')}>
            ${costUSD.toFixed(2)} <span className="text-faint font-normal">/ ${limitUSD.toFixed(0)}</span>
            {blocked && <span className="ml-2 text-xs font-semibold uppercase tracking-wide">{t('dashboard.budget_blocked')}</span>}
          </span>
        </div>
        <div className="h-[7px] bg-gray-100 rounded-full overflow-hidden">
          <div
            className={`h-full rounded-full transition-all duration-500 ${barColor}`}
            style={{ width: `${clampedPct * 100}%` }}
          />
        </div>
        {blocked && (
          <p className="text-xs text-red-600 mt-2">
            {t('dashboard.budget_unblock_hint')}
          </p>
        )}
      </div>
    </section>
  )
}

