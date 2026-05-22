import { useEffect, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Zap, AlertCircle, AlertTriangle } from 'lucide-react'
import {
  api,
  type SubscriptionProvider,
  type SubscriptionTier,
} from '../api/client'

// ExhaustEvent is the payload of the daemon's subscription_exhausted SSE
// event. Spec/05 §12.3.
interface ExhaustEvent {
  provider: string
  tier: string
  highspeed: boolean
  window_end: string  // RFC3339
}

// SubscriptionQuotaCard surfaces spec/05's subscription-quota data: every
// provider with a polled quota row gets a card showing per-tier remaining
// calls, window timing, and effective cost-per-call. When a provider has no
// OAuth credential we show a hint pointing back to the Agents page; when no
// providers are configured the card collapses to nothing.
export default function SubscriptionQuotaCard() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['subscription-status'],
    queryFn: api.subscriptionStatus,
    refetchInterval: 60_000,
  })

  // Transient banner state: holds the most recent exhaust event so the user
  // sees an attention-grabbing notice the next ~30s, then it self-clears.
  // We also force-refetch on every event so the per-tier bar updates to
  // "0 remaining" without waiting for the 60s refetch interval.
  const [exhaust, setExhaust] = useState<ExhaustEvent | null>(null)

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.addEventListener('subscription_exhausted', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as ExhaustEvent
        setExhaust(data)
        qc.invalidateQueries({ queryKey: ['subscription-status'] })
      } catch { /* ignore malformed payload */ }
    })
    es.addEventListener('subscription_quota_refreshed', () => {
      qc.invalidateQueries({ queryKey: ['subscription-status'] })
    })
    return () => es.close()
  }, [qc])

  // Auto-clear the banner after 30s. Re-running on every new event resets
  // the timer so back-to-back exhaustions stay visible.
  useEffect(() => {
    if (!exhaust) return
    const timer = setTimeout(() => setExhaust(null), 30_000)
    return () => clearTimeout(timer)
  }, [exhaust])

  const refresh = useMutation({
    mutationFn: () => api.subscriptionRefresh(),
    onSuccess: (fresh) => {
      qc.setQueryData(['subscription-status'], fresh)
    },
  })

  if (isLoading) return null
  if (!data || data.length === 0) return null

  return (
    <section
      data-testid="subscription-quota-card"
      className="bg-white border border-border rounded-2xl p-5 shadow-sm"
    >
      {exhaust && (
        <div
          data-testid="subscription-exhausted-banner"
          role="status"
          className="mb-4 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900"
        >
          <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
          <div className="flex-1">
            <p className="font-medium">
              {exhaust.provider} {exhaust.tier} quota exhausted
            </p>
            <p className="text-[11px] text-amber-700 mt-0.5">
              Routing has fallen back to per-token vendors until the window
              resets at {new Date(exhaust.window_end).toLocaleString()}.
            </p>
          </div>
          <button
            type="button"
            onClick={() => setExhaust(null)}
            aria-label="Dismiss"
            className="text-amber-700 hover:text-amber-900 text-[11px] underline underline-offset-2"
          >
            Dismiss
          </button>
        </div>
      )}

      <header className="flex items-baseline justify-between mb-4">
        <div className="flex items-center gap-2">
          <Zap className="w-4 h-4 text-amber-500" />
          <h2 className="text-sm font-semibold">Subscription Quota</h2>
        </div>
        <button
          type="button"
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          className="inline-flex items-center gap-1 text-xs text-gray-500 hover:text-gray-900 disabled:opacity-50"
        >
          <RefreshCw
            className={`w-3.5 h-3.5 ${refresh.isPending ? 'animate-spin' : ''}`}
          />
          Refresh
        </button>
      </header>

      <div className="space-y-5">
        {data.map((p) => (
          <ProviderSection key={p.provider} p={p} />
        ))}
      </div>
    </section>
  )
}

function ProviderSection({ p }: { p: SubscriptionProvider }) {
  return (
    <div>
      <div className="flex items-baseline gap-2 mb-2">
        <p className="text-sm font-medium capitalize">{p.provider}</p>
        {p.source_agent && (
          <span className="text-[11px] text-gray-400">
            via {p.source_agent}
          </span>
        )}
        {!p.oauth_present && (
          <span className="inline-flex items-center gap-1 text-[11px] text-amber-700 bg-amber-50 rounded-full px-2 py-0.5">
            <AlertCircle className="w-3 h-3" /> Static key — no quota data
          </span>
        )}
        {p.last_polled_at && (
          <span className="text-[11px] text-gray-400 ml-auto">
            polled {relativeTime(p.last_polled_at)}
          </span>
        )}
      </div>

      {p.tiers.length === 0 ? (
        <p className="text-xs text-gray-400">No tier data yet.</p>
      ) : (
        <ul className="space-y-2">
          {p.tiers.map((t) => (
            <TierRow key={`${t.tier_name}-${t.highspeed}`} tier={t} />
          ))}
        </ul>
      )}
    </div>
  )
}

function TierRow({ tier }: { tier: SubscriptionTier }) {
  const pct = tier.total > 0 ? Math.min(100, (tier.used / tier.total) * 100) : 0
  const remaining = tier.remaining
  const lowQuota = tier.total > 0 && remaining < tier.total * 0.1

  return (
    <li>
      <div className="flex items-baseline justify-between gap-2 mb-1">
        <p className="text-xs font-mono text-gray-700">
          {tier.tier_name}
          {tier.highspeed && (
            <span className="ml-1 text-[10px] text-orange-600">highspeed</span>
          )}
        </p>
        <p className="text-xs text-gray-500">
          {remaining.toLocaleString()} / {tier.total.toLocaleString()} left
        </p>
      </div>
      <div className="h-1.5 w-full bg-gray-100 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${lowQuota ? 'bg-amber-500' : 'bg-brand'}`}
          style={{ width: `${100 - pct}%` }}
        />
      </div>
      <div className="flex items-baseline justify-between mt-1">
        <p className="text-[11px] text-gray-400">
          {formatResetIn(tier.seconds_to_reset)}
        </p>
        {tier.monthly_price_usd > 0 && (
          <p className="text-[11px] text-gray-400">
            ≈ ${tier.effective_cost_per_call_usd.toFixed(6)} / call ·
            {tier.monthly_price_cny > 0
              ? ` ¥${tier.monthly_price_cny}/mo (≈ $${tier.monthly_price_usd.toFixed(2)})`
              : ` $${tier.monthly_price_usd.toFixed(2)}/mo`}
          </p>
        )}
      </div>
    </li>
  )
}

function formatResetIn(seconds: number): string {
  if (seconds <= 0) return 'window closed'
  if (seconds < 60) return `resets in ${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `resets in ${minutes}m`
  const hours = Math.floor(minutes / 60)
  const mins = minutes % 60
  if (hours < 24) return `resets in ${hours}h ${mins}m`
  const days = Math.floor(hours / 24)
  return `resets in ${days}d ${hours % 24}h`
}

function relativeTime(rfc3339: string): string {
  const ms = Date.parse(rfc3339)
  if (isNaN(ms)) return ''
  const secs = Math.floor((Date.now() - ms) / 1000)
  if (secs < 60) return `${secs}s ago`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
