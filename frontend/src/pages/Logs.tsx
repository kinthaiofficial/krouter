import { useQuery } from '@tanstack/react-query'
import { useState, useMemo } from 'react'
import { Download } from 'lucide-react'
import { api, type LogRecord } from '../api/client'

const PAGE_SIZE = 50

export default function Logs() {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const { data: logs = [], isLoading } = useQuery({
    queryKey: ['logs', 'full'],
    queryFn: () => api.logs(500),
    refetchInterval: 10_000,
  })

  const filtered = useMemo(() => {
    if (!search.trim()) return logs
    const q = search.toLowerCase()
    return logs.filter(
      (r) =>
        r.model.toLowerCase().includes(q) ||
        r.provider.toLowerCase().includes(q) ||
        (r.agent ?? '').toLowerCase().includes(q) ||
        r.id.toLowerCase().includes(q),
    )
  }, [logs, search])

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
          className="flex items-center gap-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 border border-gray-200 dark:border-gray-600 rounded-lg px-3 py-1.5"
        >
          <Download size={14} />
          Export CSV
        </button>
      </div>

      <input
        type="search"
        placeholder="Search by model, provider, agent…"
        value={search}
        onChange={(e) => { setSearch(e.target.value); setPage(0) }}
        className="w-full max-w-sm border border-gray-200 dark:border-gray-600 rounded-lg px-3 py-1.5 text-sm bg-white dark:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
      />

      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-sm text-gray-400">Loading…</div>
        ) : filtered.length === 0 ? (
          <div className="p-8 text-center text-sm text-gray-400">No records found.</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b border-gray-100 dark:border-gray-700">
                <tr className="text-left text-xs text-gray-400">
                  {['Time', 'Agent', 'Model', 'Provider', 'In', 'Out', 'Cost', 'Lat', 'Status'].map((h) => (
                    <th key={h} className="px-4 py-2 font-medium whitespace-nowrap">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50 dark:divide-gray-700/50">
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
              className="px-3 py-1 rounded border border-gray-200 dark:border-gray-600 disabled:opacity-40"
            >
              Prev
            </button>
            <span className="px-2 py-1">{page_ + 1} / {totalPages}</span>
            <button
              disabled={page_ >= totalPages - 1}
              onClick={() => setPage(page_ + 1)}
              className="px-3 py-1 rounded border border-gray-200 dark:border-gray-600 disabled:opacity-40"
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
    <tr className="hover:bg-gray-50 dark:hover:bg-gray-750">
      <td className="px-4 py-1.5 text-gray-400 text-xs whitespace-nowrap">{new Date(r.ts).toLocaleString()}</td>
      <td className="px-4 py-1.5">{r.agent ?? '—'}</td>
      <td className="px-4 py-1.5 font-mono text-xs">{r.model}</td>
      <td className="px-4 py-1.5">{r.provider}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.input_tokens.toLocaleString()}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.output_tokens.toLocaleString()}</td>
      <td className="px-4 py-1.5 text-xs text-right font-mono">${r.cost_usd.toFixed(4)}</td>
      <td className="px-4 py-1.5 text-xs text-right">{r.latency_ms}ms</td>
      <td className="px-4 py-1.5 text-xs">
        <span className={ok ? 'text-green-600 dark:text-green-400' : 'text-red-500'}>
          {r.status_code}
        </span>
      </td>
    </tr>
  )
}
