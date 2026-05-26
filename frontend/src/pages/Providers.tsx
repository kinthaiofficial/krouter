import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  CheckCircle, XCircle, AlertCircle, Plus, Trash2,
  ChevronDown, ChevronRight, RefreshCw, Zap,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  api,
  type ProviderInfo, type AddProviderBody, type ProviderModelRow,
  type SubscriptionProvider, type SubscriptionTier,
} from '../api/client'
import { statusCodeMeaning } from '../lib/statusCode'
import { PageHeader } from '../components/ui'

export default function Providers() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [showAdd, setShowAdd] = useState(false)
  const { data: providers = [], isLoading, isError } = useQuery<ProviderInfo[]>({
    queryKey: ['providers'],
    queryFn: api.providers,
    // 60s — no longer live-reordering by usage, so don't poll aggressively.
    refetchInterval: 60_000,
  })

  // Subscription quotas (MiniMax and any future subscription providers).
  // Surface them on the matching Provider card so the user has one place
  // to inspect "everything we know about this provider" — replaces the
  // separate Dashboard SubscriptionQuotaCard.
  const { data: subscriptions = [] } = useQuery<SubscriptionProvider[]>({
    queryKey: ['subscription-status'],
    queryFn: api.subscriptionStatus,
    refetchInterval: 60_000,
  })

  // Live-update on the daemon's subscription SSE events so the card refreshes
  // immediately when MiniMax just hit a quota wall, etc.
  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    const refresh = () => qc.invalidateQueries({ queryKey: ['subscription-status'] })
    es.addEventListener('subscription_exhausted', refresh)
    es.addEventListener('subscription_quota_refreshed', refresh)
    return () => es.close()
  }, [qc])

  const subByProvider = useMemo(() => {
    const m = new Map<string, SubscriptionProvider>()
    for (const s of subscriptions) m.set(s.provider, s)
    return m
  }, [subscriptions])

  const active = providers.filter((p) => p.configured)
  const inactive = providers.filter((p) => !p.configured)

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader
        title={t('providers.title')}
        subtitle={t('providers.subtitle')}
        right={
          <button
            onClick={() => setShowAdd(true)}
            className="flex items-center gap-1.5 text-sm font-medium border border-line-strong bg-card rounded-lg px-3 py-1.5 text-muted hover:border-brand hover:text-brand-ink transition-colors"
          >
            <Plus size={14} />
            {t('providers.add')}
          </button>
        }
      />

      {isLoading ? (
        <p className="text-sm text-gray-400">{t('common.loading')}</p>
      ) : isError ? (
        <p className="text-sm text-red-500">{t('providers.load_failed')}</p>
      ) : (
        <>
          {active.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs text-gray-500 font-semibold uppercase tracking-wide">{t('providers.active')}</p>
              {active.map((p) => (
                <ProviderCard key={p.name} provider={p} subscription={subByProvider.get(p.name)} />
              ))}
            </div>
          )}
          {inactive.length > 0 && (
            <div className="space-y-2 mt-6">
              <p className="text-xs text-gray-500 font-semibold uppercase tracking-wide">{t('providers.not_configured')}</p>
              {inactive.map((p) => (
                <ProviderCard key={p.name} provider={p} subscription={subByProvider.get(p.name)} />
              ))}
            </div>
          )}
        </>
      )}

      {showAdd && <AddProviderDialog onClose={() => setShowAdd(false)} />}
    </div>
  )
}

// ─── Per-provider card ─────────────────────────────────────────────────────

function ProviderCard({
  provider: p,
  subscription,
}: {
  provider: ProviderInfo
  subscription?: SubscriptionProvider
}) {
  const { t } = useTranslation()
  const location = useLocation()
  const [open, setOpen] = useState(false)
  const cardRef = useRef<HTMLDivElement>(null)

  // Deep-link target: when the page is opened with `#provider-<name>`,
  // auto-expand that card and scroll it into view. Re-runs on hash
  // change so clicking another in-page link still works.
  useEffect(() => {
    const target = `#provider-${p.name}`
    if (location.hash === target) {
      setOpen(true)
      cardRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
  }, [location.hash, p.name])

  const fullEndpoint = (p.base_url || '') + (p.path_prefix || '')
  const healthy = p.configured && p.consecutive_failures === 0 && p.available
  const statusIcon = !p.configured ? (
    <AlertCircle size={18} className="text-gray-300" />
  ) : healthy ? (
    <CheckCircle size={18} className="text-brand" />
  ) : (
    <XCircle size={18} className="text-red-500" />
  )

  return (
    <div
      id={`provider-${p.name}`}
      ref={cardRef}
      className={[
        'rounded-xl border transition-colors scroll-mt-20',
        p.configured ? 'bg-white border-gray-200' : 'bg-gray-50 border-dashed border-gray-200',
      ].join(' ')}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full text-left px-4 py-3 flex items-center gap-3 hover:bg-gray-50/60 rounded-xl"
      >
        {open ? <ChevronDown size={14} className="text-gray-400 shrink-0" /> : <ChevronRight size={14} className="text-gray-400 shrink-0" />}
        {statusIcon}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={['font-medium text-sm', p.configured ? 'text-gray-900' : 'text-gray-500'].join(' ')}>
              {p.display_name || p.name}
            </span>
            <ProtocolBadge protocol={p.protocol} />
            {p.is_builtin && (
              <span className="text-[10px] uppercase tracking-wider text-gray-400 border border-gray-200 rounded px-1.5 py-0.5">
                {t('providers.builtin')}
              </span>
            )}
            {subscription && (
              // The subscription badge sets this provider apart from
              // pay-per-token providers — clicking the card reveals the
              // tier list with usage and reset windows below.
              <span className="inline-flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-purple-700 bg-purple-50 border border-purple-200 rounded px-1.5 py-0.5">
                <Zap size={10} />
                {t('providers.subscription_badge')}
              </span>
            )}
          </div>
          <p className="text-xs text-gray-400 font-mono truncate mt-0.5" title={fullEndpoint}>
            {fullEndpoint || `env: ${p.name.toUpperCase().replace(/-/g, '_')}_API_KEY`}
          </p>
        </div>

        <Chip label={t('providers.models')} value={p.model_count.toLocaleString()} />
        <Chip
          label={t('providers.lifetime_requests')}
          value={p.requests_total.toLocaleString()}
          tone={p.requests_total > 0 ? 'blue' : 'gray'}
        />
        <Chip
          label={t('providers.lifetime_cost')}
          value={`$${p.cost_total_usd.toFixed(2)}`}
          tone={p.cost_total_usd > 0 ? 'amber' : 'gray'}
        />
      </button>

      {open && <CardDetails p={p} fullEndpoint={fullEndpoint} subscription={subscription} />}
    </div>
  )
}

// ─── Expanded details ──────────────────────────────────────────────────────

function CardDetails({
  p,
  fullEndpoint,
  subscription,
}: {
  p: ProviderInfo
  fullEndpoint: string
  subscription?: SubscriptionProvider
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [testResult, setTestResult] = useState<{ latency_ms: number; status_code: number; ok: boolean; error?: string } | null>(null)

  const test = useMutation({
    mutationFn: () => api.testProvider(p.name),
    onSuccess: (res) => setTestResult(res),
  })
  const remove = useMutation({
    mutationFn: () => api.removeProvider(p.name),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })
  const refreshSubscription = useMutation({
    mutationFn: () => api.subscriptionRefresh(p.name),
    onSuccess: (fresh) => qc.setQueryData(['subscription-status'], fresh),
  })

  const {
    data: models = [],
    isLoading: modelsLoading,
    isError: modelsError,
  } = useQuery<ProviderModelRow[]>({
    queryKey: ['provider-models', p.name],
    queryFn: () => api.providerModels(p.name),
    staleTime: 5 * 60_000,
  })

  return (
    <div className="px-4 pb-4 pt-1 space-y-4 border-t border-gray-100">
      {/* Endpoint */}
      <DetailGrid>
        <DetailRow label={t('providers.base_url')} value={p.base_url || '—'} mono />
        <DetailRow label={t('providers.path_prefix')} value={p.path_prefix || '—'} mono />
        <DetailRow label={t('providers.full_endpoint')} value={fullEndpoint || '—'} mono />
        <DetailRow label={t('providers.protocol')} value={p.protocol} />
      </DetailGrid>

      {/* Stats */}
      {p.configured && (
        <DetailGrid>
          <DetailRow
            label={t('providers.success_rate')}
            value={`${Math.round(p.success_rate * 100)}%`}
            tone={p.success_rate >= 0.95 ? 'green' : p.success_rate >= 0.8 ? 'yellow' : 'red'}
          />
          <DetailRow
            label={t('providers.requests_today')}
            value={p.requests_today.toLocaleString()}
          />
          <DetailRow
            label={t('providers.cost_today')}
            value={`$${p.cost_today_usd.toFixed(4)}`}
          />
          <DetailRow
            label={t('providers.latency_p50_p95')}
            value={p.latency_p50_ms > 0 ? `${p.latency_p50_ms}ms / ${p.latency_p95_ms}ms` : '—'}
          />
        </DetailGrid>
      )}

      {/* Lifetime totals */}
      <DetailGrid>
        <DetailRow
          label={t('providers.lifetime_requests')}
          value={p.requests_total.toLocaleString()}
        />
        <DetailRow
          label={t('providers.lifetime_in')}
          value={p.input_tokens_total.toLocaleString()}
        />
        <DetailRow
          label={t('providers.lifetime_out')}
          value={p.output_tokens_total.toLocaleString()}
        />
        <DetailRow
          label={t('providers.lifetime_cached')}
          value={p.cached_tokens_total.toLocaleString()}
        />
        {p.cache_write_tokens_total > 0 && (
          <DetailRow
            label={t('providers.lifetime_cache_write')}
            value={p.cache_write_tokens_total.toLocaleString()}
          />
        )}
      </DetailGrid>

      {/* Recent error — failure streak with plain-language explanation of the last status code. */}
      {p.consecutive_failures > 0 && (
        <p className="text-xs text-red-600 bg-red-50 border border-red-100 rounded-lg px-3 py-2">
          {t('providers.failure_streak', {
            n: p.consecutive_failures,
            code: p.last_error_code ?? '—',
          })}
          {p.last_error_code != null && p.last_error_code > 0 && (
            <span className="block mt-0.5 text-red-700/80">
              {statusCodeMeaning(p.last_error_code, t)}
            </span>
          )}
        </p>
      )}

      {/* Subscription tiers — one row per scenario (text, voice, lyrics, …).
          Replaces the old Dashboard-level SubscriptionQuotaCard so users can
          inspect all subscription state inline with the provider. */}
      {subscription && subscription.tiers.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-baseline justify-between">
            <h3 className="text-xs uppercase tracking-wider text-gray-500 font-semibold flex items-center gap-1.5">
              <Zap size={12} className="text-purple-500" />
              {t('providers.subscription_title')}
              {subscription.source_app && (
                <span className="text-[10px] text-gray-400 normal-case tracking-normal font-normal">
                  · via {subscription.source_app}
                </span>
              )}
            </h3>
            <button
              type="button"
              onClick={() => refreshSubscription.mutate()}
              disabled={refreshSubscription.isPending}
              className="inline-flex items-center gap-1 text-xs text-gray-500 hover:text-gray-900 disabled:opacity-50"
            >
              <RefreshCw size={12} className={refreshSubscription.isPending ? 'animate-spin' : ''} />
              {t('subscription.refresh')}
            </button>
          </div>
          <ul className="space-y-2">
            {subscription.tiers.map((tier) => (
              <TierRow key={`${tier.tier_name}-${tier.highspeed}`} tier={tier} />
            ))}
          </ul>
        </div>
      )}

      {/* Models */}
      <div className="space-y-2">
        <div className="flex items-baseline justify-between">
          <h3 className="text-xs uppercase tracking-wider text-gray-500 font-semibold">
            {t('providers.models')}
          </h3>
          <span className="text-xs text-gray-400">{models.length || p.model_count} {t('providers.models')}</span>
        </div>
        {modelsLoading ? (
          <p className="text-sm text-gray-400">{t('common.loading')}</p>
        ) : modelsError ? (
          // Distinct from "no_models" — that's the genuinely-empty case;
          // this surfaces an actual fetch failure so the user doesn't
          // confuse a 500 with an empty catalog.
          <p className="text-sm text-red-500">{t('providers.models_load_failed')}</p>
        ) : models.length === 0 ? (
          <p className="text-sm text-gray-400 italic">{t('providers.no_models')}</p>
        ) : (
          <ModelsTable models={models} t={t} />
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 pt-2 border-t border-gray-50">
        {p.configured && (
          <button
            onClick={() => test.mutate()}
            disabled={test.isPending}
            className="text-xs border border-gray-200 rounded-lg px-2.5 py-1 hover:border-blue-400 hover:text-blue-600 disabled:opacity-40"
          >
            {test.isPending ? t('providers.testing') : t('providers.test')}
          </button>
        )}
        {testResult && (
          <span className={[
            'text-xs font-mono',
            testResult.ok ? 'text-green-600' : 'text-red-500',
          ].join(' ')}>
            {testResult.latency_ms}ms · {testResult.status_code}{testResult.error ? ` · ${testResult.error}` : ''}
          </span>
        )}
        {!p.configured && <SetKeyButton p={p} />}
        {!p.is_builtin && (
          <button
            onClick={() => remove.mutate()}
            disabled={remove.isPending}
            className="ml-auto text-xs text-red-400 hover:text-red-600 disabled:opacity-40 flex items-center gap-1"
          >
            <Trash2 size={13} />
            {t('providers.remove')}
          </button>
        )}
      </div>
    </div>
  )
}

// ─── Set-key inline form ───────────────────────────────────────────────────

function SetKeyButton({ p }: { p: ProviderInfo }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [expanded, setExpanded] = useState(false)
  const [key, setKey] = useState('')

  const setKeyMutation = useMutation({
    mutationFn: () => api.setProviderKey(p.name, key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      setExpanded(false)
      setKey('')
    },
  })

  if (!expanded) {
    return (
      <button
        onClick={() => setExpanded(true)}
        className="text-xs border border-blue-200 text-blue-600 rounded-lg px-2.5 py-1 hover:border-blue-400 hover:text-blue-700"
      >
        {t('providers.set_key')}
      </button>
    )
  }

  return (
    <div className="flex items-center gap-2 flex-1">
      <input
        type="password"
        value={key}
        onChange={(e) => setKey(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && key && setKeyMutation.mutate()}
        placeholder={t('providers.api_key_placeholder')}
        className="flex-1 text-xs border border-gray-200 rounded-lg px-2.5 py-1"
        autoFocus
      />
      <button
        onClick={() => setKeyMutation.mutate()}
        disabled={!key || setKeyMutation.isPending}
        className="text-xs bg-blue-600 text-white rounded-lg px-2.5 py-1 disabled:opacity-40 hover:bg-blue-700"
      >
        {setKeyMutation.isPending ? t('providers.saving') : t('providers.save')}
      </button>
      <button
        onClick={() => { setExpanded(false); setKey('') }}
        className="text-xs text-gray-500"
      >
        {t('common.cancel')}
      </button>
    </div>
  )
}

// ─── Models table ──────────────────────────────────────────────────────────

function ModelsTable({
  models,
  t,
}: {
  models: ProviderModelRow[]
  t: ReturnType<typeof useTranslation>['t']
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="text-gray-400 border-b border-gray-100">
            <th className="text-left py-1 font-normal">{t('providers.model_id')}</th>
            <th className="text-right py-1 font-normal">{t('providers.input_per_mtok')}</th>
            <th className="text-right py-1 font-normal">{t('providers.output_per_mtok')}</th>
            <th className="text-right py-1 font-normal">{t('providers.max_tokens')}</th>
          </tr>
        </thead>
        <tbody>
          {models.map((m) => (
            <tr key={m.model_id} className="border-b border-gray-50">
              <td className="py-1 font-mono text-gray-700 max-w-[260px] truncate" title={m.model_id}>{m.model_id}</td>
              <td className="py-1 text-right tabular-nums">{m.input_per_mtok > 0 ? `$${m.input_per_mtok.toFixed(2)}` : '—'}</td>
              <td className="py-1 text-right tabular-nums">{m.output_per_mtok > 0 ? `$${m.output_per_mtok.toFixed(2)}` : '—'}</td>
              <td className="py-1 text-right tabular-nums text-gray-500">
                {m.max_tokens > 0 ? m.max_tokens.toLocaleString() : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ─── Small bits ────────────────────────────────────────────────────────────

function ProtocolBadge({ protocol }: { protocol: string }) {
  const cls =
    protocol === 'anthropic' ? 'bg-orange-50 text-orange-700 border-orange-200' :
    protocol === 'openai' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' :
    'bg-gray-50 text-gray-600 border-gray-200'
  return (
    <span className={['text-[10px] uppercase tracking-wider font-semibold px-1.5 py-0.5 rounded border', cls].join(' ')}>
      {protocol}
    </span>
  )
}

function Chip({ label, value, tone = 'gray' }: { label: string; value: string; tone?: 'gray' | 'blue' | 'amber' }) {
  const cls = {
    gray: 'bg-gray-50 text-gray-700',
    blue: 'bg-blue-50 text-blue-700',
    amber: 'bg-amber-50 text-amber-700',
  }[tone]
  return (
    <div className={['hidden sm:flex shrink-0 flex-col items-end rounded-lg px-2 py-1', cls].join(' ')}>
      <span className="text-xs font-mono tabular-nums">{value}</span>
      <span className="text-[10px] text-gray-400 uppercase tracking-wider">{label}</span>
    </div>
  )
}

function DetailGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">{children}</div>
  )
}

function DetailRow({
  label,
  value,
  mono = false,
  tone,
}: {
  label: string
  value: string
  mono?: boolean
  tone?: 'green' | 'yellow' | 'red'
}) {
  const valueCls =
    tone === 'green' ? 'text-green-600' :
    tone === 'yellow' ? 'text-yellow-700' :
    tone === 'red' ? 'text-red-500' : 'text-gray-900'
  return (
    <div>
      <p className="text-[11px] uppercase tracking-wider text-gray-400">{label}</p>
      <p className={['text-sm mt-0.5 break-all', mono ? 'font-mono' : '', valueCls].join(' ')} title={value}>{value}</p>
    </div>
  )
}

// ─── Add Provider dialog (unchanged from before) ───────────────────────────

function AddProviderDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [form, setForm] = useState<AddProviderBody>({
    name: '',
    display_name: '',
    base_url: '',
    path_prefix: '',
    protocol: 'openai',
    api_key: '',
  })
  const [error, setError] = useState('')

  const add = useMutation({
    mutationFn: () => api.addProvider(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      onClose()
    },
    onError: (e: Error) => setError(e.message),
  })

  const set = (field: keyof AddProviderBody) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl p-6 w-full max-w-md space-y-4 shadow-xl">
        <div>
          <h2 className="font-semibold text-base">Add Custom Provider</h2>
          <p className="text-xs text-gray-500 mt-0.5">Any OpenAI-compatible LLM API endpoint.</p>
        </div>

        <div className="space-y-3">
          <div>
            <label className="text-xs text-gray-500 block mb-1">Name <span className="text-red-400">*</span></label>
            <input
              value={form.name}
              onChange={set('name')}
              placeholder="e.g. my-llm"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
            <p className="text-xs text-gray-400 mt-0.5">Lowercase letters, digits, - or _</p>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">Display Name</label>
            <input
              value={form.display_name}
              onChange={set('display_name')}
              placeholder="My LLM"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">Base URL <span className="text-red-400">*</span></label>
            <input
              value={form.base_url}
              onChange={set('base_url')}
              placeholder="https://api.example.com"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-gray-500 block mb-1">Path Prefix</label>
              <input
                value={form.path_prefix}
                onChange={set('path_prefix')}
                placeholder="/v1 (default)"
                className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
              />
            </div>
            <div>
              <label className="text-xs text-gray-500 block mb-1">Protocol</label>
              <select
                value={form.protocol}
                onChange={set('protocol')}
                className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400 bg-white"
              >
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
              </select>
            </div>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">API Key</label>
            <input
              type="password"
              value={form.api_key}
              onChange={set('api_key')}
              placeholder="Optional — can add later"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
        </div>

        {error && <p className="text-xs text-red-500">{error}</p>}

        <div className="flex gap-2 justify-end pt-1">
          <button
            onClick={onClose}
            className="text-sm border border-gray-200 rounded-lg px-4 py-2 hover:border-gray-400"
          >
            Cancel
          </button>
          <button
            onClick={() => add.mutate()}
            disabled={!form.name || !form.base_url || add.isPending}
            className="text-sm bg-blue-600 text-white rounded-lg px-4 py-2 hover:bg-blue-700 disabled:opacity-40"
          >
            {add.isPending ? 'Adding…' : 'Add Provider'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─── Subscription tier row ────────────────────────────────────────────────

function TierRow({ tier }: { tier: SubscriptionTier }) {
  const { t } = useTranslation()
  const pct = tier.total > 0 ? Math.min(100, (tier.used / tier.total) * 100) : 0

  // Used-fill bar with traffic-light tones — matches the Budget page's
  // colour staging and the more common Western dashboard idiom of
  // showing "what's been consumed" rather than "what's left".
  const barTone =
    pct >= 95 ? 'bg-red-500'
    : pct >= 80 ? 'bg-yellow-500'
    : 'bg-emerald-500'

  return (
    <li className="rounded-lg border border-gray-200 px-3 py-2">
      <div className="flex items-baseline justify-between gap-2 mb-1 flex-wrap">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-sm font-medium text-gray-900 truncate" title={tier.tier_name}>
            {scenarioDisplayName(tier.tier_name, t)}
          </span>
          {tier.highspeed && (
            <span className="text-[10px] text-orange-600 bg-orange-50 rounded px-1">
              {t('subscription.highspeed')}
            </span>
          )}
        </div>
        <p className="text-xs font-mono text-gray-600 tabular-nums">
          {t('providers.tier_usage', {
            used: tier.used.toLocaleString(),
            total: tier.total.toLocaleString(),
            pct: pct.toFixed(0),
          })}
        </p>
      </div>
      <div className="h-1.5 w-full bg-gray-100 rounded-full overflow-hidden">
        <div
          className={['h-full rounded-full transition-all', barTone].join(' ')}
          style={{ width: `${pct}%` }}
          role="progressbar"
          aria-valuenow={Math.round(pct)}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>
      <div className="flex items-baseline justify-between mt-1 gap-2 flex-wrap text-[11px]">
        <span className="text-gray-400">
          {tier.window_start && tier.window_end
            ? `${formatLocalWindow(tier.window_start)}–${formatLocalWindow(tier.window_end)}`
            : ''}
          {tier.seconds_to_reset > 0 && (
            <span className="ml-1.5">· {formatResetIn(tier.seconds_to_reset, t)}</span>
          )}
        </span>
        {tier.monthly_price_usd > 0 && (
          <span className="text-gray-400">
            ≈ ${tier.effective_cost_per_call_usd.toFixed(6)} / call
          </span>
        )}
      </div>
    </li>
  )
}

// scenarioDisplayName maps known MiniMax (and future-provider) scenario
// names to friendly i18n keys. Unknown names fall back to the raw value.
//
// MiniMax's token-plan endpoint returns scenarios as opaque model_name
// strings (e.g. "MiniMax-M*", "speech_synthesis"). We catch the well-known
// shapes here and let everything else through unchanged.
function scenarioDisplayName(raw: string, t: ReturnType<typeof useTranslation>['t']): string {
  const lower = raw.toLowerCase()
  if (raw.startsWith('MiniMax-M')) return t('providers.scenario_text')
  if (lower.includes('speech') || lower.startsWith('tts') || lower.includes('t2a')) {
    return t('providers.scenario_speech')
  }
  if (lower.includes('lyric')) return t('providers.scenario_lyrics')
  if (lower.includes('music')) return t('providers.scenario_music')
  if (lower.includes('mcp') && lower.includes('image')) return t('providers.scenario_mcp_image')
  if (lower.includes('mcp') && (lower.includes('search') || lower.includes('web'))) {
    return t('providers.scenario_mcp_search')
  }
  if (lower.includes('image') || lower.includes('vision')) return t('providers.scenario_image')
  return raw
}

function formatLocalWindow(rfc3339: string): string {
  const d = new Date(rfc3339)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatResetIn(seconds: number, t: ReturnType<typeof useTranslation>['t']): string {
  if (seconds <= 0) return t('subscription.window_closed')
  if (seconds < 60) return t('subscription.resets_in', { time: `${seconds}s` })
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return t('subscription.resets_in', { time: `${minutes}m` })
  const hours = Math.floor(minutes / 60)
  const mins = minutes % 60
  if (hours < 24) return t('subscription.resets_in', { time: `${hours}h ${mins}m` })
  const days = Math.floor(hours / 24)
  return t('subscription.resets_in', { time: `${days}d ${hours % 24}h` })
}
