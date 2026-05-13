import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api, type LogRecord, type Preset } from '../api/client'
import PresetSwitcher from '../components/PresetSwitcher'
import QuotaBar from '../components/QuotaBar'

export default function Dashboard() {
  const qc = useQueryClient()
  const { data: budget } = useQuery({ queryKey: ['budget'], queryFn: api.budget })
  const { data: quotas } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: logsData } = useQuery({ queryKey: ['logs'], queryFn: () => api.logs(20) })
  const { data: presetData } = useQuery({ queryKey: ['preset'], queryFn: api.preset })

  const [recentLogs, setRecentLogs] = useState<LogRecord[]>([])
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    if (logsData) setRecentLogs(logsData)
  }, [logsData])

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    esRef.current = es
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        setRecentLogs((prev) => [rec, ...prev].slice(0, 50))
        qc.invalidateQueries({ queryKey: ['budget'] })
        qc.invalidateQueries({ queryKey: ['quota'] })
      } catch { /* ignore */ }
    })
    es.addEventListener('settings_changed', () => {
      qc.invalidateQueries({ queryKey: ['preset'] })
    })
    return () => es.close()
  }, [qc])

  const setPreset = useMutation({
    mutationFn: (p: Preset) => api.setPreset(p),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['preset'] }),
  })

  return (
    <div className="p-6 space-y-5 max-w-5xl mx-auto">
      <h1 className="text-lg font-semibold">Dashboard</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <PresetSwitcher
          current={presetData?.preset ?? 'balanced'}
          onSelect={(p) => setPreset.mutate(p)}
        />

        {/* Today stats */}
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-2">
          <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400">Today</h2>
          <div className="flex gap-6">
            <Stat label="requests" value={String(budget?.requests_today ?? 0)} />
            <Stat label="saved" value={`$${(budget?.savings_today_usd ?? 0).toFixed(3)}`} />
            <Stat label="spent" value={`$${(budget?.cost_today_usd ?? 0).toFixed(3)}`} />
          </div>
        </div>
      </div>

      {/* Quota bars */}
      {quotas && quotas.length > 0 && (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-4">
          <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400">Quota</h2>
          {quotas.map((q) => <QuotaBar key={q.window} quota={q} />)}
        </div>
      )}

      {/* Recent requests */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5">
        <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400 mb-4">Recent Requests</h2>
        <RequestTable logs={recentLogs} />
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-2xl font-bold">{value}</p>
      <p className="text-xs text-gray-500">{label}</p>
    </div>
  )
}

function RequestTable({ logs }: { logs: LogRecord[] }) {
  if (logs.length === 0) return <p className="text-sm text-gray-400">No requests yet.</p>
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs text-gray-400 border-b border-gray-100 dark:border-gray-700">
            {['Time', 'Agent', 'Model', 'Provider', 'Cost', 'Latency'].map((h) => (
              <th key={h} className="pb-2 pr-4 last:text-right">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-50 dark:divide-gray-700">
          {logs.map((log) => (
            <tr key={log.id} className="hover:bg-gray-50 dark:hover:bg-gray-750">
              <td className="py-1.5 pr-4 text-gray-400 text-xs whitespace-nowrap">{new Date(log.ts).toLocaleTimeString()}</td>
              <td className="py-1.5 pr-4">{log.agent ?? '—'}</td>
              <td className="py-1.5 pr-4 font-mono text-xs">{log.model}</td>
              <td className="py-1.5 pr-4">{log.provider}</td>
              <td className="py-1.5 pr-4 text-xs">${log.cost_usd.toFixed(4)}</td>
              <td className="py-1.5 text-right text-xs">{log.latency_ms}ms</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
