import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link2, Link2Off, RefreshCw, ChevronDown, ChevronUp, TerminalSquare, RotateCcw } from 'lucide-react'

interface AgentStats {
  requests_today: number
  cost_today_usd: number
  savings_today_usd: number
}

interface AgentStatus {
  name: string
  config_path?: string
  cli_path?: string
  connected: boolean
  providers?: string[]
  stats: AgentStats
}

interface LogRow {
  id: string
  ts: string
  protocol: string
  requested_model?: string
  provider: string
  model: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  latency_ms: number
  status_code: number
}

const AGENT_LABELS: Record<string, string> = {
  'openclaw': 'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor': 'Cursor',
  'hermes': 'Hermes',
}

export default function Agents() {
  const qc = useQueryClient()
  const [expandedLogs, setExpandedLogs] = useState<string | null>(null)

  const { data: agents = [], isLoading, refetch } = useQuery<AgentStatus[]>({
    queryKey: ['agents'],
    queryFn: () =>
      fetch('/internal/agents', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<AgentStatus[]>
      }),
    refetchInterval: 30_000,
  })

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Agents</h1>
        <button
          onClick={() => { refetch(); qc.invalidateQueries({ queryKey: ['agents'] }) }}
          className="flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-900 px-3 py-1.5 rounded-lg hover:bg-surface transition-colors"
        >
          <RefreshCw size={14} />
          Re-detect
        </button>
      </div>

      {isLoading ? (
        <p className="text-sm text-gray-400">Detecting agents…</p>
      ) : agents.length === 0 ? (
        <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-8 text-center space-y-1">
          <p className="text-sm text-gray-400">No supported AI agents detected.</p>
          <p className="text-xs text-gray-400">KRouter supports OpenClaw, Claude Code, Cursor, and Hermes.</p>
        </div>
      ) : (
        <div className="space-y-3">
          {agents.map((a) => (
            <AgentCard
              key={a.name}
              agent={a}
              logsExpanded={expandedLogs === a.name}
              onToggleLogs={() => setExpandedLogs(expandedLogs === a.name ? null : a.name)}
              onMutationSuccess={() => qc.invalidateQueries({ queryKey: ['agents'] })}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function AgentCard({
  agent: a,
  logsExpanded,
  onToggleLogs,
  onMutationSuccess,
}: {
  agent: AgentStatus
  logsExpanded: boolean
  onToggleLogs: () => void
  onMutationSuccess: () => void
}) {
  const label = AGENT_LABELS[a.name] ?? a.name

  const connectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/agents/${a.name}/connect`, {
        method: 'POST',
        credentials: 'include',
      }).then((r) => {
        if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
        return r.json()
      }),
    onSuccess: onMutationSuccess,
  })

  const disconnectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/agents/${a.name}/disconnect`, {
        method: 'POST',
        credentials: 'include',
      }).then((r) => {
        if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
        return r.json()
      }),
    onSuccess: onMutationSuccess,
  })

  const isBusy = connectMutation.isPending || disconnectMutation.isPending

  const { data: logs = [], isLoading: logsLoading } = useQuery<LogRow[]>({
    queryKey: ['agent-logs', a.name],
    queryFn: () =>
      fetch(`/internal/logs?agent=${encodeURIComponent(a.name)}&n=50`, { credentials: 'include' })
        .then((r) => r.json() as Promise<LogRow[]>),
    enabled: logsExpanded,
    staleTime: 10_000,
  })

  return (
    <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
      <div className="p-4">
        <div className="flex items-start gap-3">
          <div className="shrink-0 mt-0.5">
            {a.connected ? (
              <Link2 size={18} className="text-brand" />
            ) : (
              <Link2Off size={18} className="text-gray-300" />
            )}
          </div>

          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <p className="font-medium text-sm">{label}</p>
              <span className={[
                'text-xs px-1.5 py-0.5 rounded-full font-medium',
                a.connected ? 'bg-brand-light text-brand' : 'bg-gray-100 text-gray-400',
              ].join(' ')}>
                {a.connected ? 'Connected' : 'Not connected'}
              </span>
            </div>
            {a.config_path && (
              <p className="text-xs text-gray-400 font-mono truncate mt-0.5">{a.config_path}</p>
            )}
            {a.cli_path && (
              <p className="text-xs text-gray-400 font-mono truncate mt-0.5">{a.cli_path}</p>
            )}
            {a.providers && a.providers.length > 0 && (
              <p className="text-xs text-gray-500 mt-1">
                Providers: {a.providers.join(', ')}
              </p>
            )}
          </div>

          <div className="shrink-0">
            {a.connected ? (
              <button
                onClick={() => disconnectMutation.mutate()}
                disabled={isBusy}
                className="text-xs px-3 py-1.5 rounded-lg bg-red-50 text-red-600 hover:bg-red-100 disabled:opacity-50 transition-colors font-medium"
              >
                {disconnectMutation.isPending ? 'Disconnecting…' : 'Disconnect'}
              </button>
            ) : (
              <button
                onClick={() => connectMutation.mutate()}
                disabled={isBusy}
                className="text-xs px-3 py-1.5 rounded-lg bg-brand-light text-brand hover:bg-green-100 disabled:opacity-50 transition-colors font-medium"
              >
                {connectMutation.isPending ? 'Connecting…' : 'Connect'}
              </button>
            )}
          </div>
        </div>

        {(connectMutation.error || disconnectMutation.error) && (
          <p className="mt-2 text-xs text-red-500 pl-7">
            {(connectMutation.error as Error)?.message ?? (disconnectMutation.error as Error)?.message}
          </p>
        )}

        {connectMutation.isSuccess && a.name === 'claude-code' && (
          <div className="mt-2 ml-7 flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <TerminalSquare size={12} className="shrink-0" />
            Open a new terminal for the env vars to take effect.
          </div>
        )}
        {connectMutation.isSuccess && (a.name === 'openclaw' || a.name === 'cursor') && (
          <div className="mt-2 ml-7 flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <RotateCcw size={12} className="shrink-0" />
            Restart {label} to apply the new routing config.
          </div>
        )}

        <div className="mt-3 flex items-center gap-4 text-xs text-gray-500 border-t border-gray-100 pt-3">
          <div>
            <span className="font-medium text-gray-900">{a.stats.requests_today}</span>
            {' '}requests today
          </div>
          <div>
            <span className="font-medium text-gray-900">${a.stats.cost_today_usd.toFixed(4)}</span>
            {' '}cost
          </div>
          {a.stats.savings_today_usd > 0.000001 && (
            <div>
              <span className="font-medium text-brand">${a.stats.savings_today_usd.toFixed(4)}</span>
              {' '}saved
            </div>
          )}
          <button
            onClick={onToggleLogs}
            className="ml-auto flex items-center gap-1 hover:text-gray-900 transition-colors"
          >
            {logsExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
            {logsExpanded ? 'Hide logs' : 'Show logs'}
          </button>
        </div>
      </div>

      {logsExpanded && (
        <div className="border-t border-gray-100 bg-gray-50 px-4 py-3">
          {logsLoading ? (
            <p className="text-xs text-gray-400">Loading logs…</p>
          ) : logs.length === 0 ? (
            <p className="text-xs text-gray-400">No requests logged for this agent yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-gray-400 text-left">
                    <th className="pb-2 font-medium w-24">Time</th>
                    <th className="pb-2 font-medium">Model</th>
                    <th className="pb-2 font-medium">Provider</th>
                    <th className="pb-2 font-medium text-right">Tokens</th>
                    <th className="pb-2 font-medium text-right">Cost</th>
                    <th className="pb-2 font-medium text-right">Latency</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {logs.map((log) => (
                    <tr key={log.id} className={log.status_code >= 400 ? 'text-red-500' : 'text-gray-700'}>
                      <td className="py-1.5 pr-2 text-gray-400 tabular-nums">
                        {new Date(log.ts).toLocaleTimeString()}
                      </td>
                      <td className="py-1.5 pr-2 font-mono truncate max-w-[140px]" title={log.model || log.requested_model}>
                        {log.model || log.requested_model || '—'}
                      </td>
                      <td className="py-1.5 pr-2">{log.provider || '—'}</td>
                      <td className="py-1.5 pr-2 text-right tabular-nums">
                        {(log.input_tokens + log.output_tokens).toLocaleString()}
                      </td>
                      <td className="py-1.5 pr-2 text-right tabular-nums">
                        ${log.cost_usd.toFixed(5)}
                      </td>
                      <td className="py-1.5 text-right tabular-nums">
                        {log.latency_ms}ms
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
