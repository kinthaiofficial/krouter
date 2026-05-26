import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ArrowRight, ChevronDown, ChevronRight, Info, TrendingDown, TrendingUp } from 'lucide-react'
import { api, type LogRecord, type ProviderInfo } from '../api/client'
import { statusCodeMeaning } from '../lib/statusCode'

// ─── Public components used by both Router and Logs pages ──────────────────

interface DecisionCardProps {
  rec: LogRecord
  pulse?: boolean
  showLatestBadge?: boolean
}

export function DecisionCard({ rec, pulse = false, showLatestBadge = false }: DecisionCardProps) {
  const { t } = useTranslation()
  const ok = rec.status_code >= 200 && rec.status_code < 300

  const { data: providers = [] } = useQuery<ProviderInfo[]>({
    queryKey: ['providers', 'endpoints'],
    queryFn: api.providers,
    staleTime: 5 * 60_000,
  })
  const endpointFor = (name: string) => {
    const p = providers.find((x) => x.name === name)
    if (!p) return ''
    return (p.base_url ?? '') + (p.path_prefix ?? '')
  }

  const requestedModel = rec.requested_model || rec.model
  const routedModel = rec.model
  const modelChanged = requestedModel !== routedModel
  const requestedProvider = rec.requested_provider || rec.provider
  const routedProvider = rec.provider
  const providerChanged = requestedProvider !== routedProvider
  const baseline = rec.baseline_cost_usd
  const actual = rec.cost_usd
  const priced = baseline !== undefined && baseline > 0
  const savings = priced ? baseline! - actual : undefined
  const savingsPct = priced ? (savings! / baseline!) * 100 : undefined

  return (
    <div
      className={[
        'bg-white rounded-xl overflow-hidden border transition-all duration-500',
        pulse
          ? 'border-green-400 shadow-xl shadow-green-100/60'
          : 'border-gray-200 shadow-sm hover:shadow-md',
      ].join(' ')}
    >
      {/* ── Card header ─────────────────────────────────────────────── */}
      <div className="bg-gray-50 border-b border-gray-200 px-5 pt-3.5 pb-3">
        {/* Row 1: badges + meta + status */}
        <div className="flex items-center gap-2 flex-wrap">
          {showLatestBadge && (
            <span className="bg-green-500 text-white text-[10px] font-bold tracking-wider px-2 py-0.5 rounded-full">
              {t('router.latest_badge')}
            </span>
          )}
          <span className="text-xs text-gray-400 tabular-nums font-mono">
            {new Date(rec.ts).toLocaleString()}
          </span>
          {rec.app && (
            <>
              <span className="text-gray-300 text-xs">·</span>
              <span className="inline-flex items-center text-xs font-semibold bg-indigo-50 text-indigo-700 border border-indigo-200 px-2 py-0.5 rounded-full">
                {rec.app}
              </span>
            </>
          )}
          {savings !== undefined && savings > 0 && savingsPct !== undefined && (
            <>
              <span className="text-gray-300 text-xs">·</span>
              <span className="inline-flex items-center gap-1 bg-green-100 text-green-700 text-xs font-semibold px-2.5 py-0.5 rounded-full border border-green-200">
                <TrendingDown size={11} />
                ${savings.toFixed(4)} · {savingsPct.toFixed(1)}%
              </span>
            </>
          )}
          {savings !== undefined && savings < -0.000001 && savingsPct !== undefined && (
            <>
              <span className="text-gray-300 text-xs">·</span>
              <span className="inline-flex items-center gap-1 bg-red-50 text-red-600 text-xs font-semibold px-2.5 py-0.5 rounded-full border border-red-200">
                <TrendingUp size={11} />
                ${Math.abs(savings).toFixed(4)} · {Math.abs(savingsPct).toFixed(1)}%
              </span>
            </>
          )}
          <span className="ml-auto flex items-center gap-2 shrink-0">
            <span className="text-[10px] font-mono text-gray-300" title={rec.id}>
              #{rec.id.slice(-8)}
            </span>
            <StatusPill code={rec.status_code} ok={ok} />
          </span>
        </div>

      </div>

      {/* ── Card body ───────────────────────────────────────────────── */}
      <div className="px-5 py-5 space-y-5">
        {/* Request section */}
        <Section label={t('router.section_request')}>
          <div className="grid grid-cols-1 md:grid-cols-[1fr_auto_1fr] gap-3 items-stretch">
            <RequestCard
              label={t('router.req_requested')}
              tone="gray"
              endpoint={endpointFor(requestedProvider)}
              protocol={rec.protocol}
              provider={requestedProvider}
              model={requestedModel}
              inputPerMTok={rec.requested_input_per_mtok}
              outputPerMTok={rec.requested_output_per_mtok}
              cacheReadPerMTok={rec.requested_cache_read_per_mtok}
              estInputTokens={rec.input_tokens}
              estOutputTokens={rec.output_tokens}
              t={t}
            />
            <div className="hidden md:flex items-center justify-center text-gray-300">
              <ArrowRight size={18} />
            </div>
            <RequestCard
              label={t('router.req_routed')}
              tone={modelChanged ? 'green' : providerChanged ? 'blue' : 'gray'}
              endpoint={endpointFor(routedProvider)}
              protocol={rec.protocol}
              provider={routedProvider}
              model={routedModel}
              highlightModel={modelChanged}
              inputPerMTok={rec.routed_input_per_mtok}
              outputPerMTok={rec.routed_output_per_mtok}
              cacheReadPerMTok={rec.routed_cache_read_per_mtok}
              highlightPrice={modelChanged}
              estInputTokens={rec.input_tokens}
              estOutputTokens={rec.output_tokens}
              t={t}
            />
          </div>
        </Section>

        {/* Response section */}
        <Section label={t('router.section_response')}>
          <div className="grid grid-cols-1 md:grid-cols-[1fr_auto_1fr] gap-3 items-stretch">
            <ResponseCard
              label={t('router.resp_projected')}
              tone="gray"
              inputTokens={rec.input_tokens}
              outputTokens={rec.output_tokens}
              cachedTokens={rec.cached_tokens ?? 0}
              cacheWriteTokens={rec.cache_write_tokens ?? 0}
              cost={baseline}
              latencyMS={rec.latency_ms}
              t={t}
              isBaseline
            />
            <div className="hidden md:flex items-center justify-center text-gray-300">
              <ArrowRight size={18} />
            </div>
            <ResponseCard
              label={t('router.resp_actual')}
              tone={savings !== undefined && savings > 0 ? 'green' : 'gray'}
              inputTokens={rec.input_tokens}
              outputTokens={rec.output_tokens}
              cachedTokens={rec.cached_tokens ?? 0}
              cacheWriteTokens={rec.cache_write_tokens ?? 0}
              cost={actual}
              highlightCost
              latencyMS={rec.latency_ms}
              t={t}
            />
          </div>
        </Section>

        {/* Error row */}
        {!ok && (
          <span className="w-full text-red-600 text-xs flex items-start gap-1.5">
            <Info size={12} className="shrink-0 mt-0.5" aria-hidden />
            <span>
              <span className="font-mono">HTTP {rec.status_code}</span>
              {' — '}
              {statusCodeMeaning(rec.status_code, t)}
            </span>
          </span>
        )}
        {!ok && rec.error_message && (
          <p className="text-xs text-red-600 font-mono">{rec.error_message}</p>
        )}
      </div>
    </div>
  )
}

// ─── Section wrapper ──────────────────────────────────────────────────────

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <p className="text-[11px] uppercase tracking-wider text-gray-400 font-semibold">{label}</p>
      {children}
    </div>
  )
}

// ─── Request card ─────────────────────────────────────────────────────────

interface RequestCardProps {
  label: string
  tone: 'gray' | 'green' | 'blue'
  endpoint: string
  protocol: string
  provider: string
  model: string
  highlightModel?: boolean
  inputPerMTok?: number
  outputPerMTok?: number
  cacheReadPerMTok?: number
  highlightPrice?: boolean
  estInputTokens: number
  estOutputTokens: number
  t: ReturnType<typeof useTranslation>['t']
}

function RequestCard({
  label, tone, endpoint, protocol, provider, model,
  highlightModel = false, inputPerMTok, outputPerMTok, cacheReadPerMTok,
  highlightPrice = false, estInputTokens, estOutputTokens, t,
}: RequestCardProps) {
  const toneCls = {
    gray:  'bg-gray-50 border-gray-200',
    blue:  'bg-blue-50 border-blue-200',
    green: 'bg-green-50 border-green-200',
  }[tone]
  const cacheWritePerMTok = inputPerMTok != null && inputPerMTok > 0 ? inputPerMTok * 1.25 : undefined
  return (
    <div className={['rounded-lg border px-4 py-3', toneCls].join(' ')}>
      <p className="text-[10px] uppercase tracking-wider text-gray-400 font-bold mb-2.5">{label}</p>
      <dl className="space-y-1.5 text-sm">
        <Field k={t('router.endpoint')} v={endpoint || '—'} mono dim={!endpoint} />
        <Field k={t('router.protocol')} v={protocol || '—'} />
        <Field k={t('router.provider')} v={provider || '—'} />
        <Field k={t('router.model')} v={model || '—'} mono highlight={highlightModel} big={highlightModel} />
        <Field
          k={t('router.price_input')}
          v={inputPerMTok != null && inputPerMTok > 0 ? `$${inputPerMTok.toFixed(2)} / 1M` : '—'}
          mono highlight={highlightPrice}
        />
        <Field
          k={t('router.price_output')}
          v={outputPerMTok != null && outputPerMTok > 0 ? `$${outputPerMTok.toFixed(2)} / 1M` : '—'}
          mono highlight={highlightPrice}
        />
        {cacheReadPerMTok != null && cacheReadPerMTok > 0 && (
          <Field k={t('router.price_cache_read')} v={`$${cacheReadPerMTok.toFixed(2)} / 1M`} mono />
        )}
        {cacheWritePerMTok != null && cacheReadPerMTok != null && cacheReadPerMTok > 0 && (
          <Field k={t('router.price_cache_write')} v={`$${cacheWritePerMTok.toFixed(2)} / 1M`} mono />
        )}
        <Field
          k={t('router.est_tokens')}
          v={t('router.tokens_in_out', { in: estInputTokens.toLocaleString(), out: estOutputTokens.toLocaleString() })}
          mono dim
        />
      </dl>
    </div>
  )
}

// ─── Response card ─────────────────────────────────────────────────────────

interface ResponseCardProps {
  label: string
  tone: 'gray' | 'green' | 'blue'
  inputTokens: number
  outputTokens: number
  cachedTokens: number
  cacheWriteTokens?: number
  cost?: number
  highlightCost?: boolean
  latencyMS: number
  isBaseline?: boolean
  t: ReturnType<typeof useTranslation>['t']
}

function ResponseCard({
  label, tone, inputTokens, outputTokens, cachedTokens, cacheWriteTokens = 0,
  cost, highlightCost = false, latencyMS, isBaseline = false, t,
}: ResponseCardProps) {
  const toneCls = {
    gray:  'bg-gray-50 border-gray-200',
    blue:  'bg-blue-50 border-blue-200',
    green: 'bg-green-50 border-green-200',
  }[tone]
  const totalInput = inputTokens + cachedTokens + cacheWriteTokens
  const hitRateStr = totalInput > 0
    ? `${((cachedTokens / totalInput) * 100).toFixed(1)}%`
    : '—'
  return (
    <div className={['rounded-lg border px-4 py-3', toneCls].join(' ')}>
      <p className="text-[10px] uppercase tracking-wider text-gray-400 font-bold mb-2.5">{label}</p>
      <dl className="space-y-1.5 text-sm">
        <Field k={t('router.tokens_in')}    v={inputTokens.toLocaleString()} mono />
        <Field k={t('router.tokens_out')}   v={outputTokens.toLocaleString()} mono />
        <Field k={t('router.tokens_read')}  v={cachedTokens.toLocaleString()} mono />
        <Field k={t('router.tokens_write')} v={cacheWriteTokens.toLocaleString()} mono />
        <Field k={t('router.cache_hit_rate')} v={hitRateStr} mono />
        <Field
          k={t('router.actual_cost')}
          v={cost != null ? `$${cost.toFixed(4)}` : '—'}
          mono highlight={highlightCost} big={highlightCost}
        />
        {!isBaseline && (
          <Field
            k={t('router.latency')}
            v={t('router.latency_ms', { ms: latencyMS.toLocaleString() })}
            mono dim
          />
        )}
      </dl>
    </div>
  )
}

// ─── Field row ─────────────────────────────────────────────────────────────

interface FieldProps {
  k: string
  v: string
  mono?: boolean
  big?: boolean
  highlight?: boolean
  dim?: boolean
}

function Field({ k, v, mono = false, big = false, highlight = false, dim = false }: FieldProps) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-xs text-gray-400 w-24 shrink-0">{k}</dt>
      <dd
        className={[
          mono ? 'font-mono' : '',
          big ? 'text-base font-semibold' : 'text-sm',
          highlight ? 'text-green-700' : '',
          dim ? 'text-gray-400' : 'text-gray-800',
          'truncate',
        ].join(' ')}
        title={v}
      >
        {v}
      </dd>
    </div>
  )
}

// ─── Collapsed one-liner row (Logs page) ─────────────────────────────────

export function DecisionRow({
  r,
  initiallyOpen = false,
}: {
  r: LogRecord
  initiallyOpen?: boolean
}) {
  const [open, setOpen] = useState(initiallyOpen)
  const requested = r.requested_model ?? r.model
  const routed = r.model
  const modelChanged = requested !== routed
  const ok = r.status_code >= 200 && r.status_code < 300

  return (
    <li>
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-3 px-4 py-2 text-sm hover:bg-gray-50 text-left"
      >
        {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        <span className="text-xs text-gray-400 w-32 shrink-0 tabular-nums">
          {new Date(r.ts).toLocaleString()}
        </span>
        <span className="w-24 shrink-0 truncate text-gray-700">{r.app ?? '—'}</span>
        <span className="font-mono text-xs text-gray-500 truncate">{requested}</span>
        <ArrowRight size={12} className="text-gray-300 shrink-0" />
        <span className={['font-mono text-xs truncate', modelChanged ? 'text-green-700 font-semibold' : 'text-gray-500'].join(' ')}>
          {r.provider} / {routed}
        </span>
        <span className="ml-auto flex items-center gap-3 shrink-0">
          <span className="text-xs font-mono text-gray-500">${r.cost_usd.toFixed(4)}</span>
          <span className="text-xs text-gray-400 tabular-nums">{r.latency_ms}ms</span>
          <StatusPill code={r.status_code} ok={ok} />
        </span>
      </button>
      {open && (
        <div className="px-4 pb-4 pt-1">
          <DecisionCard rec={r} />
        </div>
      )}
    </li>
  )
}

// ─── Status pill ──────────────────────────────────────────────────────────

function StatusPill({ code, ok }: { code: number; ok: boolean }) {
  const { t } = useTranslation()
  const meaning = statusCodeMeaning(code, t)
  return (
    <span
      title={`${code} — ${meaning}`}
      className={[
        'inline-flex items-center gap-1 text-xs font-mono px-2 py-0.5 rounded-full cursor-help font-semibold',
        ok ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-600',
      ].join(' ')}
    >
      {code}
      {!ok && <Info size={10} className="opacity-60" aria-hidden />}
    </span>
  )
}
