import { useEffect, useRef, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, CheckCircle2, History, Wallet } from 'lucide-react'
import { api, type Budget, type BudgetEvent, type Settings as ISettings } from '../api/client'

export default function BudgetPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()

  const { data: settings } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const { data: budget } = useQuery({
    queryKey: ['budget'],
    queryFn: api.budget,
    refetchInterval: 30_000,
  })
  const { data: events = [] } = useQuery({
    queryKey: ['budget', 'events'],
    queryFn: () => api.budgetEvents(50),
    refetchInterval: 60_000,
  })

  const save = useMutation({
    mutationFn: (patch: Partial<ISettings>) => api.patchSettings(patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  // Live updates — invalidate budget data when a transition fires. Debounced
  // so a burst of events (e.g. 80% and 95% crossed in the same minute, or
  // back-to-back broadcasts during a restart) collapses to a single refetch.
  const refetchTimerRef = useRef<number | null>(null)
  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.addEventListener('budget_warning', () => {
      if (refetchTimerRef.current !== null) {
        window.clearTimeout(refetchTimerRef.current)
      }
      refetchTimerRef.current = window.setTimeout(() => {
        qc.invalidateQueries({ queryKey: ['budget'] })
        qc.invalidateQueries({ queryKey: ['budget', 'events'] })
        refetchTimerRef.current = null
      }, 500)
    })
    return () => {
      es.close()
      if (refetchTimerRef.current !== null) {
        window.clearTimeout(refetchTimerRef.current)
        refetchTimerRef.current = null
      }
    }
  }, [qc])

  // Local input state so the user can type freely; we PATCH on blur/Enter.
  const dailyLimit = settings?.budget_warnings?.daily ?? 0
  const weeklyLimit = settings?.budget_warnings?.weekly ?? 0
  const [dailyDraft, setDailyDraft] = useState<string>('')
  const [weeklyDraft, setWeeklyDraft] = useState<string>('')
  useEffect(() => { if (settings) setDailyDraft(String(dailyLimit)) }, [settings, dailyLimit])
  useEffect(() => { if (settings) setWeeklyDraft(String(weeklyLimit)) }, [settings, weeklyLimit])

  // commitDaily / commitWeekly handle blur or Enter on the limit inputs.
  // On invalid input (non-numeric, negative, etc.) we reset the draft back
  // to the last known good value rather than silently dropping the save —
  // otherwise the user sees their bad text sit in the box and has no idea
  // why nothing was persisted.
  function commitDaily() {
    const v = parseFloat(dailyDraft)
    if (!Number.isFinite(v) || v < 0) {
      setDailyDraft(String(dailyLimit))
      return
    }
    if (v !== dailyLimit) {
      save.mutate({ budget_warnings: { ...(settings?.budget_warnings ?? {}), daily: v } })
    }
  }
  function commitWeekly() {
    const v = parseFloat(weeklyDraft)
    if (!Number.isFinite(v) || v < 0) {
      setWeeklyDraft(String(weeklyLimit))
      return
    }
    if (v !== weeklyLimit) {
      save.mutate({ budget_warnings: { ...(settings?.budget_warnings ?? {}), weekly: v } })
    }
  }

  return (
    <div className="p-6 space-y-4 max-w-3xl mx-auto">
      <div className="flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold">{t('budget.title')}</h1>
          <p className="text-xs text-gray-500 mt-0.5">{t('budget.subtitle')}</p>
        </div>
      </div>

      <SpendCard budget={budget} t={t} />

      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
        <div className="flex items-start gap-2">
          <Wallet size={18} className="text-gray-400 mt-0.5" />
          <div>
            <h2 className="text-sm font-medium">{t('budget.limits')}</h2>
            <p className="text-xs text-gray-500 mt-0.5">{t('budget.limits_detail')}</p>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <LimitInput
            label={t('budget.daily_limit')}
            value={dailyDraft}
            onChange={setDailyDraft}
            onBlur={commitDaily}
            onEnter={commitDaily}
          />
          <LimitInput
            label={t('budget.weekly_limit')}
            value={weeklyDraft}
            onChange={setWeeklyDraft}
            onBlur={commitWeekly}
            onEnter={commitWeekly}
          />
        </div>
      </section>

      <EventsTimeline events={events} t={t} />
    </div>
  )
}

// ─── Spend card ────────────────────────────────────────────────────────────

function SpendCard({
  budget,
  t,
}: {
  budget: Budget | undefined
  t: ReturnType<typeof import('react-i18next').useTranslation>['t']
}) {
  if (!budget) {
    return (
      <div className="bg-white rounded-xl border border-gray-200 p-5 text-sm text-gray-400">
        {t('common.loading')}
      </div>
    )
  }

  const dailyLimit = budget.daily_limit_usd ?? 0
  const pct = budget.daily_percent_used ?? 0
  const blocked = budget.budget_blocked ?? false
  const remaining = Math.max(0, dailyLimit - budget.cost_today_usd)

  let barColor = 'bg-green-500'
  let pillBg = 'bg-green-50 text-green-700'
  let label = t('budget.state_ok')
  if (pct >= 1) {
    barColor = 'bg-red-500'
    pillBg = 'bg-red-50 text-red-700'
    label = t('budget.state_blocked')
  } else if (pct >= 0.95) {
    barColor = 'bg-red-400'
    pillBg = 'bg-red-50 text-red-700'
    label = t('budget.state_warn_95')
  } else if (pct >= 0.80) {
    barColor = 'bg-yellow-500'
    pillBg = 'bg-yellow-50 text-yellow-700'
    label = t('budget.state_warn_80')
  }

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <h2 className="text-sm font-medium">{t('budget.today')}</h2>
        <span className={['inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-full font-medium', pillBg].join(' ')}>
          {blocked ? <AlertTriangle size={12} /> : <CheckCircle2 size={12} />}
          {label}
        </span>
      </div>

      {dailyLimit > 0 ? (
        <>
          <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
            <div
              className={['h-full transition-all duration-500', barColor].join(' ')}
              style={{ width: `${Math.min(100, pct * 100).toFixed(1)}%` }}
            />
          </div>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
            <Stat label={t('budget.spent_today')} value={`$${budget.cost_today_usd.toFixed(4)}`} />
            <Stat label={t('budget.daily_limit')} value={`$${dailyLimit.toFixed(2)}`} />
            <Stat label={t('budget.remaining')} value={`$${remaining.toFixed(4)}`} tone={blocked ? 'red' : pct >= 0.8 ? 'yellow' : 'green'} />
            <Stat label={t('budget.requests_today')} value={budget.requests_today.toLocaleString()} />
          </div>
        </>
      ) : (
        <p className="text-sm text-gray-500">{t('budget.no_limit_set')}</p>
      )}

      {budget.savings_today_usd > 0 && (
        <p className="text-xs text-gray-500">
          {t('budget.saved_today', { amount: `$${budget.savings_today_usd.toFixed(4)}` })}
        </p>
      )}

      {blocked && (
        <p className="text-xs text-red-600 bg-red-50 border border-red-100 rounded-lg px-3 py-2">
          {t('budget.blocked_hint')}
        </p>
      )}
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: 'red' | 'yellow' | 'green' }) {
  const toneCls =
    tone === 'red' ? 'text-red-600' :
    tone === 'yellow' ? 'text-yellow-700' :
    tone === 'green' ? 'text-green-700' : 'text-gray-900'
  return (
    <div>
      <p className="text-xs text-gray-500">{label}</p>
      <p className={['text-base font-mono mt-0.5', toneCls].join(' ')}>{value}</p>
    </div>
  )
}

// ─── Limit input ───────────────────────────────────────────────────────────

function LimitInput({
  label,
  value,
  onChange,
  onBlur,
  onEnter,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  onBlur: () => void
  onEnter: () => void
}) {
  return (
    <label className="block">
      <span className="text-xs text-gray-500">{label}</span>
      <div className="mt-1 flex items-center gap-2">
        <span className="text-sm text-gray-400">$</span>
        <input
          type="number"
          min={0}
          step={0.5}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onBlur={onBlur}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              onEnter()
              ;(e.target as HTMLInputElement).blur()
            }
          }}
          className="flex-1 border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
        />
      </div>
    </label>
  )
}

// ─── Events timeline ───────────────────────────────────────────────────────

function EventsTimeline({
  events,
  t,
}: {
  events: BudgetEvent[]
  t: ReturnType<typeof import('react-i18next').useTranslation>['t']
}) {
  return (
    <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
      <div className="flex items-start gap-2">
        <History size={18} className="text-gray-400 mt-0.5" />
        <div>
          <h2 className="text-sm font-medium">{t('budget.history')}</h2>
          <p className="text-xs text-gray-500 mt-0.5">{t('budget.history_detail')}</p>
        </div>
      </div>

      {events.length === 0 ? (
        <p className="text-sm text-gray-500 italic">{t('budget.history_empty')}</p>
      ) : (
        <ul className="divide-y divide-gray-50">
          {events.map((e) => (
            <li key={e.id} className="flex items-center gap-3 py-2 text-sm">
              <EventBadge type={e.event_type} t={t} />
              <span className="text-xs text-gray-500 tabular-nums w-40 shrink-0">
                {new Date(e.ts).toLocaleString()}
              </span>
              <span className="text-xs font-mono text-gray-600">
                ${e.daily_cost_usd.toFixed(4)} / ${e.daily_limit_usd.toFixed(2)}
              </span>
              <span className="text-xs text-gray-500 ml-auto tabular-nums">
                {(e.daily_percent * 100).toFixed(1)}%
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function EventBadge({
  type,
  t,
}: {
  type: BudgetEvent['event_type']
  t: ReturnType<typeof import('react-i18next').useTranslation>['t']
}) {
  const cfg: Record<BudgetEvent['event_type'], { cls: string; label: string }> = {
    warning_80: { cls: 'bg-yellow-50 text-yellow-700 border-yellow-200', label: t('budget.event_warning_80') },
    warning_95: { cls: 'bg-orange-50 text-orange-700 border-orange-200', label: t('budget.event_warning_95') },
    blocked:    { cls: 'bg-red-50 text-red-700 border-red-200',          label: t('budget.event_blocked') },
    unblocked:  { cls: 'bg-green-50 text-green-700 border-green-200',    label: t('budget.event_unblocked') },
  }
  const c = cfg[type]
  return (
    <span className={['text-[10px] uppercase tracking-wider font-semibold px-1.5 py-0.5 rounded border', c.cls].join(' ')}>
      {c.label}
    </span>
  )
}
