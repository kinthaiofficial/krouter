import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowRight, ChevronDown, ChevronRight } from 'lucide-react'
import type { LogRecord } from '../api/client'

// ─── Public components used by both Router and Logs pages ──────────────────

interface DecisionCardProps {
  rec: LogRecord
  // pulse: brief green-border highlight when a new SSE event just arrived.
  pulse?: boolean
  // showLatestBadge: render the green LATEST chip in the header. Router page
  // sets this for the top card; Logs page never does.
  showLatestBadge?: boolean
}

export function DecisionCard({ rec, pulse = false, showLatestBadge = false }: DecisionCardProps) {
  const { t } = useTranslation()
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
      <div className="px-5 py-3 flex items-center gap-3 border-b border-gray-100 flex-wrap">
        {showLatestBadge && (
          <span className="bg-green-500 text-white text-[10px] font-semibold tracking-wider px-2 py-0.5 rounded">
            {t('router.latest_badge')}
          </span>
        )}
        <span className="text-sm text-gray-700">{new Date(rec.ts).toLocaleString()}</span>
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

        <div className="mt-5 pt-4 border-t border-gray-100 flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-gray-500">
          <span>
            {t('router.tokens_breakdown', {
              in: rec.input_tokens.toLocaleString(),
              out: rec.output_tokens.toLocaleString(),
              cached: (rec.cached_tokens ?? 0).toLocaleString(),
            })}
          </span>
          <span>·</span>
          <span>{t('router.latency_ms', { ms: rec.latency_ms.toLocaleString() })}</span>
          {!modelChanged && (
            <span className="ml-auto text-gray-400 italic">{t('router.no_change')}</span>
          )}
          {rec.error_message && (
            <span className="w-full mt-1 text-red-600 font-mono text-xs">
              {rec.error_message}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}

/* Collapsed one-liner that expands into a full DecisionCard.
 *
 * Used by:
 *   - Router page's history section
 *   - Logs page rows (everything-expandable)
 *
 * Optional initiallyOpen prop is for tests / deep links.
 */
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
          <DecisionCard rec={r} />
        </div>
      )}
    </li>
  )
}

// ─── Private helpers ───────────────────────────────────────────────────────

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
