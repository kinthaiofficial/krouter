import { useQuery } from '@tanstack/react-query'
import { useState, useMemo, useEffect, useRef } from 'react'
import { Download } from 'lucide-react'
import { api, type LogRecord } from '../api/client'

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
  const [search, setSearch] = useState('')
  const [agentFilter, setAgentFilter] = useState('')
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
  const { data: fetchedLogs = [], isLoading } = useQuery({
    queryKey: ['logs', 'full', agentFilter, fromDate, toDate],
    queryFn: () => {
      if (fromDate && toDate) {
        return api.logsInRange(fromDate, toDate, agentFilter || undefined)
      }
      return api.logs(500, agentFilter || undefined)
    },
    staleTime: fromDate && toDate ? 30_000 : Infinity,
  })

  // Accumulator that starts from the fetched data and grows via SSE.
  const [liveLogs, setLiveLogs] = useState<LogRecord[]>([])
  useEffect(() => { setLiveLogs(fetchedLogs) }, [fetchedLogs])

  // SSE — single stable connection; filter applied per-event via ref.
  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        const filter = agentFilterRef.current
        if (!filter || rec.agent === filter) {
          setLiveLogs((prev) => [rec, ...prev].slice(0, 2000))
        }
      } catch { /* ignore malformed events */ }
    })
    return () => es.close()
  }, []) // stable — no deps

  const filtered = useMemo(() => {
    if (!search.trim()) return liveLogs
    const q = search.toLowerCase()
    return liveLogs.filter(
      (r) =>
        r.model.toLowerCase().includes(q) ||
        r.provider.toLowerCase().includes(q) ||
        (r.agent ?? '').toLowerCase().includes(q) ||
        r.id.toLowerCase().includes(q),
    )
  }, [liveLogs, search])

  const totalPages = Math.ceil(filtered.length / PAGE_SIZE)
  const page_ = Math.min(page, Math.max(0, totalPages - 1))
  const rows = filtered.slice(page_ * PAGE_SIZE, (page_ + 1) * PAGE_SIZE)

  function exportCSV() {
    const header = 'id,time,agent,model,provider,input_tokens,output_tokens,cost_usd,latency_ms,status_code\n'
    const body = filtered
      .map((r) =>
        [r.id, r.ts, r.agent ?? '', r.model, r.provider, r.input_tokens, r.output_tokens, r.cost_usd, r.latency_ms, r.status_code].join(','),
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
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Request Logs</h1>
        <button
          onClick={exportCSV}
          className="flex items-center gap-1.5 text-sm text-gray-600 hover:text-gray-900 border border-gray-200 rounded-lg px-3 py-1.5"
        >
          <Download size={14} />
          Export CSV
        </button>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <input
          type="search"
          placeholder="Search by model, provider, agent…"
          value={search}
          onChange={(e) => { setSearch(e.target.value); setPage(0) }}
          className="w-full max-w-xs border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />

        {agentsRaw.length > 0 && (
          <select
            value={agentFilter}
            onChange={(e) => { setAgentFilter(e.target.value); setPage(0) }}
            className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
          >
            <option value="">All agents</option>
            {agentsRaw.map((a) => (
              <option key={a.name} value={a.name}>{a.label}</option>
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
          {filtered.length} records · {fromDate && toDate ? 'filtered' : 'live'}
        </span>
      </div>

      <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-sm text-gray-400">Loading…</div>
        ) : filtered.length === 0 ? (
          <div className="p-8 text-center text-sm text-gray-400">No records found.</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b border-gray-100">
                <tr className="text-left text-xs text-gray-400">
                  {['Time', 'Agent', 'Model', 'Provider', 'In', 'Out', 'Cost', 'Lat', 'Status'].map((h) => (
                    <th key={h} className="px-4 py-2 font-medium whitespace-nowrap">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {rows.map((r) => (
                  <LogRow key={r.id} r={r} />
                ))}
              </tbody>
            </table>
          </div>
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
              Prev
            </button>
            <span className="px-2 py-1">{page_ + 1} / {totalPages}</span>
            <button
              disabled={page_ >= totalPages - 1}
              onClick={() => setPage(page_ + 1)}
              className="px-3 py-1 rounded border border-gray-200 disabled:opacity-40"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function LogRow({ r }: { r: LogRecord }) {
  const ok = r.status_code >= 200 && r.status_code < 300
  return (
    <tr className="hover:bg-gray-50">
      <td className="px-4 py-1.5 text-gray-400 text-xs whitespace-nowrap">{new Date(r.ts).toLocaleString()}</td>
      <td className="px-4 py-1.5">{r.agent ?? '—'}</td>
      <td className="px-4 py-1.5 font-mono text-xs">{r.model}</td>
      <td className="px-4 py-1.5">{r.provider}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.input_tokens.toLocaleString()}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.output_tokens.toLocaleString()}</td>
      <td className="px-4 py-1.5 text-xs text-right font-mono">${r.cost_usd.toFixed(4)}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.latency_ms}ms</td>
      <td className="px-4 py-1.5 text-xs">
        <span className={ok ? 'text-green-600' : 'text-red-500'}>
          {r.status_code}
        </span>
      </td>
    </tr>
  )
}
