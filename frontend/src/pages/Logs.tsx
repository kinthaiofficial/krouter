import { useQuery } from '@tanstack/react-query'
import { useState, useMemo, useEffect, useRef } from 'react'
import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, type LogRecord } from '../api/client'
import { DecisionRow } from '../components/RoutingDecision'
import { PageHeader } from '../components/ui'

const PAGE_SIZE = 50

interface AgentOption {
  name: string
  label: string
}

const AGENT_LABELS: Record<string, string> = {
  'openclaw': 'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor': 'Cursor',
  'hermes': 'Hermes',
}

export default function Logs() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [agentFilter, setAgentFilter] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('')
  const [page, setPage] = useState(0)
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const agentFilterRef = useRef(agentFilter)
  useEffect(() => { agentFilterRef.current = agentFilter }, [agentFilter])

  // Detected agents for the dropdown.
  const { data: agentsRaw = [] } = useQuery<AgentOption[]>({
    queryKey: ['agents-names'],
    queryFn: () =>
      fetch('/internal/agents', { credentials: 'include' })
        .then((r) => r.json())
        .then((list: { name: string }[]) =>
          list.map((a) => ({ name: a.name, label: AGENT_LABELS[a.name] ?? a.name }))
        ),
    staleTime: 60_000,
  })

  // Initial log fetch — keyed by agentFilter and date range so it refetches on change.
  // NOTE: do NOT default `data` to []; the new array reference each render
  // would re-fire the seed-merge useEffect and create a setState loop.
  const { data: fetchedLogs, isLoading } = useQuery({
    queryKey: ['logs', 'full', agentFilter, fromDate, toDate],
    queryFn: () => {
      if (fromDate && toDate) {
        return api.logsInRange(fromDate, toDate, agentFilter || undefined)
      }
      return api.logs(500, agentFilter || undefined)
    },
    staleTime: fromDate && toDate ? 30_000 : Infinity,
  })

  const [liveLogs, setLiveLogs] = useState<LogRecord[]>([])
  useEffect(() => {
    if (fetchedLogs) setLiveLogs(fetchedLogs)
  }, [fetchedLogs])

  // SSE — single stable connection; filter applied per-event via ref.
  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        const filter = agentFilterRef.current
        if (!filter || rec.agent === filter) {
          setLiveLogs((prev) => {
            if (prev.some((r) => r.id === rec.id)) return prev
            return [rec, ...prev].slice(0, 2000)
          })
        }
      } catch { /* ignore malformed events */ }
    })
    return () => es.close()
  }, [])

  // Derive the protocol filter options from the data itself. Previously we
  // hardcoded ['openai', 'anthropic'], which silently missed any new
  // protocol the backend added (gemini / bedrock / etc.). Now we walk
  // liveLogs once and expose whatever protocols the user actually has —
  // works even before the catalogue endpoint exists for new protocols.
  const protocolOptions = useMemo(() => {
    const seen = new Set<string>()
    for (const r of liveLogs) {
      if (r.protocol) seen.add(r.protocol)
    }
    return Array.from(seen).sort()
  }, [liveLogs])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return liveLogs.filter((r) => {
      if (protocolFilter && r.protocol !== protocolFilter) return false
      if (!q) return true
      return (
        r.model.toLowerCase().includes(q) ||
        (r.requested_model ?? '').toLowerCase().includes(q) ||
        r.provider.toLowerCase().includes(q) ||
        (r.agent ?? '').toLowerCase().includes(q) ||
        r.id.toLowerCase().includes(q)
      )
    })
  }, [liveLogs, search, protocolFilter])

  const totalPages = Math.ceil(filtered.length / PAGE_SIZE)
  const page_ = Math.min(page, Math.max(0, totalPages - 1))
  const rows = filtered.slice(page_ * PAGE_SIZE, (page_ + 1) * PAGE_SIZE)

  function exportCSV() {
    const header =
      'id,time,agent,protocol,requested_model,routed_model,provider,input_tokens,output_tokens,cached_tokens,cost_usd,latency_ms,status_code\n'
    const body = filtered
      .map((r) =>
        [
          r.id,
          r.ts,
          r.agent ?? '',
          r.protocol,
          r.requested_model ?? '',
          r.model,
          r.provider,
          r.input_tokens,
          r.output_tokens,
          r.cached_tokens ?? 0,
          r.cost_usd,
          r.latency_ms,
          r.status_code,
        ].join(','),
      )
      .join('\n')
    const blob = new Blob([header + body], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `krouter-logs-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader
        title={t('logs.title')}
        subtitle={t('logs.subtitle')}
        right={
          <button
            onClick={exportCSV}
            className="flex items-center gap-1.5 text-sm font-medium text-muted hover:text-ink border border-line-strong bg-card rounded-lg px-3 py-1.5 transition-colors"
          >
            <Download size={14} />
            {t('logs.export_csv')}
          </button>
        }
      />

      <div className="flex items-center gap-3 flex-wrap">
        <input
          type="search"
          placeholder={t('logs.search_placeholder')}
          value={search}
          onChange={(e) => { setSearch(e.target.value); setPage(0) }}
          className="w-full max-w-xs border border-line-strong rounded-lg px-3 py-1.5 text-sm bg-card focus:outline-none focus:ring-2 focus:ring-brand/40 focus:border-brand"
        />

        {agentsRaw.length > 0 && (
          <select
            value={agentFilter}
            onChange={(e) => { setAgentFilter(e.target.value); setPage(0) }}
            className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
          >
            <option value="">{t('logs.all_agents')}</option>
            {agentsRaw.map((a) => (
              <option key={a.name} value={a.name}>{a.label}</option>
            ))}
          </select>
        )}

        {protocolOptions.length > 0 && (
          <select
            value={protocolFilter}
            onChange={(e) => { setProtocolFilter(e.target.value); setPage(0) }}
            className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
          >
            <option value="">{t('logs.all_protocols')}</option>
            {protocolOptions.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        )}

        <input
          type="date"
          value={fromDate}
          onChange={(e) => { setFromDate(e.target.value); setPage(0) }}
          className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
        />
        <input
          type="date"
          value={toDate}
          onChange={(e) => { setToDate(e.target.value); setPage(0) }}
          className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
        />

        <span className="text-xs text-gray-400 ml-auto">
          {t('logs.records_summary', {
            total: filtered.length,
            filtered: fromDate && toDate ? 'filtered' : 'live',
          })}
        </span>
      </div>

      <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-sm text-gray-400">{t('common.loading')}</div>
        ) : filtered.length === 0 ? (
          <div className="p-8 text-center text-sm text-gray-400">{t('logs.no_records')}</div>
        ) : (
          <ul className="divide-y divide-gray-50">
            {rows.map((r) => (
              <DecisionRow key={r.id} r={r} />
            ))}
          </ul>
        )}
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-between text-sm text-gray-500">
          <span>{filtered.length} records</span>
          <div className="flex gap-2">
            <button
              disabled={page_ === 0}
              onClick={() => setPage(page_ - 1)}
              className="px-3 py-1 rounded border border-gray-200 disabled:opacity-40"
            >
              {t('logs.prev')}
            </button>
            <span className="px-2 py-1">{page_ + 1} / {totalPages}</span>
            <button
              disabled={page_ >= totalPages - 1}
              onClick={() => setPage(page_ + 1)}
              className="px-3 py-1 rounded border border-gray-200 disabled:opacity-40"
            >
              {t('logs.next')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
