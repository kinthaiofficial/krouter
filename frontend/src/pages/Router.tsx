import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, ArrowRight, CircleDot } from 'lucide-react'
import { api, type LogRecord } from '../api/client'

const HISTORY_CAP = 50

export default function Router() {
  const { t } = useTranslation()
  const [sseAlive, setSseAlive] = useState(false)
  const [showHistory, setShowHistory] = useState(false)

  // Seed from the last 50 records — same source the Logs page reads from.
  // NOTE: do NOT default `data` to []; the new array reference each render
  // would re-fire the seed-merge useEffect and create a setState loop.
  const { data: seed } = useQuery({
    queryKey: ['router', 'seed'],
    queryFn: () => api.logs(HISTORY_CAP),
    staleTime: Infinity,
  })

  const [feed, setFeed] = useState<LogRecord[]>([])
  useEffect(() => {
    if (seed) setFeed(seed)
  }, [seed])

  // Track id of the most-recent record so we can pulse-highlight when a new one arrives.
  const latestIdRef = useRef<string | null>(null)
  const [pulseId, setPulseId] = useState<string | null>(null)

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.onopen = () => setSseAlive(true)
    es.onerror = () => setSseAlive(false)
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        setFeed((prev) => {
          if (prev.some((r) => r.id === rec.id)) return prev
          return [rec, ...prev].slice(0, HISTORY_CAP)
        })
        latestIdRef.current = rec.id
        setPulseId(rec.id)
        window.setTimeout(() => {
          setPulseId((cur) => (cur === rec.id ? null : cur))
        }, 1500)
      } catch { /* ignore malformed events */ }
    })
    return () => es.close()
  }, [])

  const latest = feed[0]
  const history = feed.slice(1)

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <div className="flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold">{t('router.title')}</h1>
          <p className="text-xs text-gray-400 mt-0.5">{t('router.subtitle')}</p>
        </div>
        <LiveBadge alive={sseAlive} t={t} />
      </div>

      {!latest ? (
        <EmptyState t={t} />
      ) : (
        <LatestCard rec={latest} pulse={pulseId === latest.id} t={t} />
      )}

      {history.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200">
          <button
            onClick={() => setShowHistory((v) => !v)}
            className="w-full flex items-center justify-between px-4 py-3 text-sm text-gray-600 hover:bg-gray-50 rounded-xl"
          >
            <span className="flex items-center gap-2">
              {showHistory ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              {showHistory
                ? t('router.hide_history')
                : t('router.show_n_more', { n: history.length })}
            </span>
          </button>
          {showHistory && (
            <ul className="divide-y divide-gray-50 border-t border-gray-100">
              {history.map((r) => (
                <HistoryRow key={r.id} r={r} t={t} />
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Live status pill ──────────────────────────────────────────────────────

function LiveBadge({ alive, t }: { alive: boolean; t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full font-medium',
        alive ? 'bg-green-50 text-green-700' : 'bg-gray-100 text-gray-500',
      ].join(' ')}
    >
      <span
        className={[
          'w-1.5 h-1.5 rounded-full',
          alive ? 'bg-green-500 animate-pulse' : 'bg-gray-400',
        ].join(' ')}
      />
      {alive ? t('router.live') : t('router.offline')}
    </span>
  )
}

// ─── Empty state ───────────────────────────────────────────────────────────

function EmptyState({ t }: { t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 px-6 py-12 text-center">
      <CircleDot size={28} className="mx-auto text-gray-300 mb-3" />
      <p className="text-sm font-medium text-gray-700">{t('router.waiting')}</p>
      <p className="text-xs text-gray-400 mt-1">{t('router.waiting_hint')}</p>
    </div>
  )
}

// ─── Latest (expanded) card ────────────────────────────────────────────────

function LatestCard({
  rec,
  pulse,
  t,
}: {
  rec: LogRecord
  pulse: boolean
  t: ReturnType<typeof useTranslation>['t']
}) {
  const requested = rec.requested_model ?? rec.model
  const routed = rec.model
  const modelChanged = requested !== routed
  const ok = rec.status_code >= 200 && rec.status_code < 300

  return (
    <div
      className={[
        'bg-white rounded-xl border-2 transition-all duration-500',
        pulse ? 'border-green-400 shadow-lg shadow-green-100' : 'border-gray-200',
      ].join(' ')}
    >
      {/* Header row */}
      <div className="px-5 py-3 flex items-center gap-3 border-b border-gray-100">
        <span className="bg-green-500 text-white text-[10px] font-semibold tracking-wider px-2 py-0.5 rounded">
          {t('router.latest_badge')}
        </span>
        <span className="text-sm text-gray-700">{new Date(rec.ts).toLocaleTimeString()}</span>
        <span className="text-sm text-gray-500">·</span>
        <span className="text-sm font-medium text-gray-700">{rec.agent ?? '—'}</span>
        <span className="text-sm text-gray-500">·</span>
        <span className="text-xs font-mono text-gray-400 truncate" title={rec.id}>
          {rec.id}
        </span>
        <span className="ml-auto">
          <StatusPill code={rec.status_code} ok={ok} />
        </span>
      </div>

      {/* Diff body */}
      <div className="px-5 py-5">
        <div className="grid grid-cols-[1fr_auto_1fr] gap-4 items-stretch">
          <SidePanel
            label={t('router.agent_requested')}
            tone="gray"
            rows={[
              { k: t('router.protocol'), v: rec.protocol },
              { k: t('router.model'), v: requested, mono: true },
            ]}
          />
          <div className="flex items-center justify-center text-gray-400">
            <ArrowRight size={20} />
          </div>
          <SidePanel
            label={t('router.krouter_routed')}
            tone={modelChanged ? 'green' : 'blue'}
            rows={[
              { k: t('router.provider'), v: rec.provider },
              { k: t('router.model'), v: routed, mono: true, highlight: modelChanged },
              { k: t('router.cost'), v: `$${rec.cost_usd.toFixed(4)}`, mono: true },
            ]}
          />
        </div>

        {/* Footer with tokens + latency */}
        <div className="mt-5 pt-4 border-t border-gray-100 flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-gray-500">
          <span>
            {t('router.tokens_breakdown', {
              in: rec.input_tokens.toLocaleString(),
              out: rec.output_tokens.toLocaleString(),
              cached: (0).toLocaleString(),
            })}
          </span>
          <span>·</span>
          <span>{t('router.latency_ms', { ms: rec.latency_ms.toLocaleString() })}</span>
          {!modelChanged && (
            <span className="ml-auto text-gray-400 italic">{t('router.no_change')}</span>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Side panel (requested / routed) ───────────────────────────────────────

interface RowSpec {
  k: string
  v: string
  mono?: boolean
  highlight?: boolean
}

function SidePanel({
  label,
  tone,
  rows,
}: {
  label: string
  tone: 'gray' | 'green' | 'blue'
  rows: RowSpec[]
}) {
  const toneCls = {
    gray: 'bg-gray-50 border-gray-200',
    blue: 'bg-blue-50 border-blue-200',
    green: 'bg-green-50 border-green-200',
  }[tone]
  return (
    <div className={['rounded-lg border px-4 py-3', toneCls].join(' ')}>
      <p className="text-[11px] uppercase tracking-wider text-gray-500 font-semibold mb-2">{label}</p>
      <dl className="space-y-1.5">
        {rows.map((row) => (
          <div key={row.k} className="flex items-baseline gap-2">
            <dt className="text-xs text-gray-500 w-16 shrink-0">{row.k}</dt>
            <dd
              className={[
                'text-sm',
                row.mono ? 'font-mono' : '',
                row.highlight ? 'font-semibold text-green-700' : 'text-gray-900',
              ].join(' ')}
            >
              {row.v}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

// ─── Status pill (200 / 401 / etc.) ────────────────────────────────────────

function StatusPill({ code, ok }: { code: number; ok: boolean }) {
  return (
    <span
      className={[
        'text-xs font-mono px-2 py-0.5 rounded',
        ok ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600',
      ].join(' ')}
    >
      {code}
    </span>
  )
}

// ─── Collapsed history row ─────────────────────────────────────────────────

function HistoryRow({
  r,
  t,
}: {
  r: LogRecord
  t: ReturnType<typeof useTranslation>['t']
}) {
  const [open, setOpen] = useState(false)
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
        <span className="text-xs text-gray-400 w-20 shrink-0 tabular-nums">
          {new Date(r.ts).toLocaleTimeString()}
        </span>
        <span className="w-24 shrink-0 truncate text-gray-700">{r.agent ?? '—'}</span>
        <span className="font-mono text-xs text-gray-500 truncate">{requested}</span>
        <ArrowRight size={12} className="text-gray-300 shrink-0" />
        <span
          className={[
            'font-mono text-xs truncate',
            modelChanged ? 'text-green-700 font-semibold' : 'text-gray-500',
          ].join(' ')}
        >
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
          <LatestCard rec={r} pulse={false} t={t} />
        </div>
      )}
    </li>
  )
}
