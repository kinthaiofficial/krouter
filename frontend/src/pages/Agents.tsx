import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Link2, Link2Off, RefreshCw, ChevronDown, ChevronUp,
  TerminalSquare, RotateCcw, History, Check, Power, Edit2,
} from 'lucide-react'
import {
  api,
  type BackupInfo, type AgentDiff,
  type SupportedAgent, type ConfiguredAgent,
} from '../api/client'
import { PageHeader } from '../components/ui'

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

interface UnifiedAgent {
  id: string
  displayName: string
  supported?: SupportedAgent
  config?: ConfiguredAgent
  status?: AgentStatus
}

const AGENT_LABELS: Record<string, string> = {
  'openclaw': 'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor': 'Cursor',
  'hermes': 'Hermes',
}

// ─── DiffModal ─────────────────────────────────────────────────────────────

function DiffModal({ diff, onConfirm, onCancel, loading }: {
  diff: AgentDiff
  onConfirm: () => void
  onCancel: () => void
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[80vh] flex flex-col">
        <div className="p-5 border-b border-gray-100">
          <h2 className="font-semibold text-sm">{t('agents.preview_changes')}</h2>
          <p className="text-xs text-gray-500 mt-0.5">{t('agents.preview_detail')}</p>
        </div>
        <div className="flex-1 overflow-auto p-5 grid grid-cols-2 gap-4 min-h-0">
          <div>
            <p className="text-xs text-gray-400 mb-1 font-medium">{t('agents.before')}</p>
            <pre className="text-xs bg-red-50 border border-red-100 rounded-lg p-3 overflow-auto max-h-64 whitespace-pre-wrap">{diff.before}</pre>
          </div>
          <div>
            <p className="text-xs text-gray-400 mb-1 font-medium">{t('agents.after')}</p>
            <pre className="text-xs bg-green-50 border border-green-100 rounded-lg p-3 overflow-auto max-h-64 whitespace-pre-wrap">{diff.after}</pre>
          </div>
        </div>
        <div className="p-5 border-t border-gray-100 flex justify-end gap-3">
          <button onClick={onCancel} className="px-4 py-2 text-sm border border-gray-200 rounded-lg hover:bg-gray-50">
            {t('common.cancel')}
          </button>
          <button
            onClick={onConfirm}
            disabled={loading}
            className="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? t('agents.connecting') : t('agents.confirm_connect')}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─── BackupsPanel ──────────────────────────────────────────────────────────

function BackupsPanel({ agentName }: { agentName: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: backups = [], isLoading } = useQuery<BackupInfo[]>({
    queryKey: ['agent-backups', agentName],
    queryFn: () => api.agentBackups(agentName),
  })

  const restore = useMutation({
    mutationFn: (filename: string) => api.agentRestore(agentName, filename),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agent-backups', agentName] })
    },
  })

  if (isLoading) return <p className="text-xs text-gray-500 py-2">{t('agents.backups_loading')}</p>
  if (backups.length === 0) return <p className="text-xs text-gray-500 py-2">{t('agents.backups_empty')}</p>

  return (
    <div className="space-y-1">
      {backups.map((b) => (
        <div key={b.filename} className="flex items-center gap-3 py-1.5 text-xs border-b border-gray-50 last:border-0">
          <span className="flex-1 font-mono text-gray-600 truncate" title={b.filename}>
            {new Date(b.created_at).toLocaleString()}
          </span>
          <span className="text-gray-500">{b.size_kb > 0 ? `${b.size_kb} KB` : '< 1 KB'}</span>
          <button
            onClick={() => {
              if (window.confirm(t('agents.restore_confirm', { date: new Date(b.created_at).toLocaleString() }))) {
                restore.mutate(b.filename)
              }
            }}
            disabled={restore.isPending}
            className="text-blue-600 hover:text-blue-800 disabled:opacity-40 font-medium"
          >
            {t('agents.restore')}
          </button>
        </div>
      ))}
      {restore.isError && <p className="text-xs text-red-500">{t('agents.restore_failed')}</p>}
    </div>
  )
}

// ─── Page ──────────────────────────────────────────────────────────────────

export default function Agents() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [expandedLogs, setExpandedLogs] = useState<string | null>(null)

  const { data: supported = [], isLoading: loadingSupported } = useQuery<SupportedAgent[]>({
    queryKey: ['agents-supported'],
    queryFn: api.agentsSupported,
    staleTime: 60_000,
  })
  const { data: configured = [] } = useQuery<ConfiguredAgent[]>({
    queryKey: ['agents-configured'],
    queryFn: api.agentsConfigured,
    refetchInterval: 15_000,
  })
  const { data: statuses = [], isLoading: loadingStatuses, refetch } = useQuery<AgentStatus[]>({
    queryKey: ['agents'],
    queryFn: () =>
      fetch('/internal/agents', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<AgentStatus[]>
      }),
    refetchInterval: 30_000,
  })

  const cfgByID = new Map(configured.map((c) => [c.agent_id, c]))
  const statusByName = new Map(statuses.map((a) => [a.name, a]))

  // Build unified list starting from the scanner registry (canonical order).
  // Any legacy agent that appears in /internal/agents but not in supported
  // (e.g. older daemon build) is appended at the end.
  const seenIDs = new Set<string>()
  const agents: UnifiedAgent[] = []

  for (const s of supported) {
    seenIDs.add(s.agent_id)
    agents.push({
      id: s.agent_id,
      displayName: s.display_name,
      supported: s,
      config: cfgByID.get(s.agent_id),
      status: statusByName.get(s.agent_id),
    })
  }
  for (const st of statuses) {
    if (!seenIDs.has(st.name)) {
      agents.push({
        id: st.name,
        displayName: AGENT_LABELS[st.name] ?? st.name,
        status: st,
      })
    }
  }

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['agents'] })
    qc.invalidateQueries({ queryKey: ['agents-configured'] })
  }

  return (
    <div className="p-6 space-y-4 max-w-3xl mx-auto">
      <PageHeader
        title={t('agents.title')}
        right={
          <button
            onClick={() => { refetch(); invalidate() }}
            className="flex items-center gap-1.5 text-sm font-medium text-muted hover:text-ink border border-line-strong bg-card px-3 py-1.5 rounded-lg transition-colors"
          >
            <RefreshCw size={14} />
            {t('agents.re_detect')}
          </button>
        }
      />

      {(loadingSupported || loadingStatuses) ? (
        <p className="text-sm text-gray-500">{t('agents.detecting')}</p>
      ) : agents.length === 0 ? (
        <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-8 text-center space-y-1">
          <p className="text-sm text-gray-500">{t('agents.none_detected')}</p>
          <p className="text-xs text-gray-500">{t('agents.none_detail')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {agents.map((a) => (
            <UnifiedAgentCard
              key={a.id}
              agent={a}
              logsExpanded={expandedLogs === a.id}
              onToggleLogs={() => setExpandedLogs(expandedLogs === a.id ? null : a.id)}
              onMutationSuccess={invalidate}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── UnifiedAgentCard ──────────────────────────────────────────────────────

function UnifiedAgentCard({
  agent: a,
  logsExpanded,
  onToggleLogs,
  onMutationSuccess,
}: {
  agent: UnifiedAgent
  logsExpanded: boolean
  onToggleLogs: () => void
  onMutationSuccess: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()

  const enabled = a.config?.enabled ?? false
  const connected = a.status?.connected ?? false
  const inherited = a.config?.inherited_count ?? 0
  const lastScannedAt = a.config?.last_scanned_at
  const lastError = a.config?.last_error

  const [editPath, setEditPath] = useState(false)
  const [pathDraft, setPathDraft] = useState(
    a.config?.config_path ?? a.supported?.default_path ?? ''
  )
  const [showBackups, setShowBackups] = useState(false)
  const [showDiff, setShowDiff] = useState(false)
  const [pendingDiff, setPendingDiff] = useState<AgentDiff | null>(null)

  // Inheritance mutations
  const enable = useMutation({
    mutationFn: () => api.agentEnable(a.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['agents-configured'] }) },
  })
  const disable = useMutation({
    mutationFn: () => api.agentDisable(a.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['agents-configured'] }) },
  })
  const rescan = useMutation({
    mutationFn: (path?: string) => api.agentRescan(a.id, path),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['agents-configured'] }) },
  })

  // Proxy connect/disconnect mutations
  const connectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/agents/${a.id}/connect`, { method: 'POST', credentials: 'include' })
        .then((r) => {
          if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
          return r.json()
        }),
    onSuccess: onMutationSuccess,
  })
  const disconnectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/agents/${a.id}/disconnect`, { method: 'POST', credentials: 'include' })
        .then((r) => {
          if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
          return r.json()
        }),
    onSuccess: onMutationSuccess,
  })
  const getDiff = useMutation({
    mutationFn: () => api.agentDiff(a.id),
    onSuccess: (diff) => { setPendingDiff(diff); setShowDiff(true) },
  })

  const isBusy =
    enable.isPending || disable.isPending || rescan.isPending ||
    connectMutation.isPending || disconnectMutation.isPending || getDiff.isPending

  const { data: logs = [], isLoading: logsLoading } = useQuery<LogRow[]>({
    queryKey: ['agent-logs', a.id],
    queryFn: () =>
      fetch(`/internal/logs?agent=${encodeURIComponent(a.id)}&n=50`, { credentials: 'include' })
        .then((r) => r.json() as Promise<LogRow[]>),
    enabled: logsExpanded,
    staleTime: 10_000,
  })

  const configPath = a.config?.config_path ?? a.supported?.default_path ?? a.status?.config_path

  return (
    <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
      <div className="p-4 space-y-3">

        {/* Header row: avatar · name · badges · action buttons */}
        <div className="flex items-start gap-3">
          {/* Avatar circle — green when enabled, gray otherwise */}
          <span
            className={`shrink-0 mt-0.5 inline-flex h-8 w-8 items-center justify-center rounded-full text-white text-[11px] font-bold tracking-tight ${
              enabled ? 'bg-brand' : 'bg-gray-300'
            }`}
          >
            {a.id.slice(0, 2).toUpperCase()}
          </span>

          <div className="flex-1 min-w-0">
            {/* Name + status badges */}
            <div className="flex items-center gap-2 flex-wrap">
              <p className="font-semibold text-sm">{a.displayName}</p>

              {/* Inheritance: enabled / disabled */}
              {enabled ? (
                <span className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald-700 bg-emerald-50 rounded-full px-2 py-0.5">
                  <Check className="w-3 h-3" /> {t('common.enabled')}
                </span>
              ) : a.config ? (
                <span className="text-[11px] font-medium text-gray-500 bg-gray-100 rounded-full px-2 py-0.5">
                  {t('common.disabled')}
                </span>
              ) : null}

              {/* Proxy: connected / not connected */}
              {connected ? (
                <span className="inline-flex items-center gap-1 text-[11px] font-medium text-brand bg-brand-light rounded-full px-2 py-0.5">
                  <Link2 className="w-3 h-3" /> {t('agents.connected')}
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-[11px] font-medium text-gray-400 bg-gray-50 rounded-full px-2 py-0.5">
                  <Link2Off className="w-3 h-3" /> {t('agents.not_connected')}
                </span>
              )}
            </div>

            {/* Config path (editable) */}
            {configPath && (
              <div className="flex items-center gap-1.5 mt-1">
                {editPath ? (
                  <input
                    type="text"
                    value={pathDraft}
                    onChange={(e) => setPathDraft(e.target.value)}
                    className="flex-1 text-xs font-mono px-2 py-0.5 border border-gray-200 rounded"
                  />
                ) : (
                  <p className="text-xs text-gray-500 font-mono truncate flex-1" title={configPath}>
                    {configPath}
                  </p>
                )}
                {a.supported && (
                  <button
                    type="button"
                    onClick={() => setEditPath((v) => !v)}
                    className="text-gray-400 hover:text-gray-700 shrink-0"
                    aria-label={editPath ? 'Cancel' : 'Edit path'}
                  >
                    <Edit2 className="w-3 h-3" />
                  </button>
                )}
              </div>
            )}

            {/* Inheritance meta: providers + last scan */}
            {a.config && (
              <p className="text-[11px] text-gray-500 mt-0.5">
                {inherited > 0
                  ? t('inheritance.providers_count', { count: inherited })
                  : enabled ? t('inheritance.no_providers') : '—'}
                {lastScannedAt && (
                  <>
                    {' · '}{t('inheritance.last_scan')}{' '}
                    <span title={new Date(lastScannedAt).toLocaleString()}>
                      {relativeTime(lastScannedAt)}
                    </span>
                  </>
                )}
              </p>
            )}

            {lastError && (
              <p className="text-[11px] text-red-600 mt-0.5" title={lastError}>
                {t('common.error')}: {lastError}
              </p>
            )}
          </div>

          {/* Action buttons cluster */}
          <div className="shrink-0 flex items-center gap-1.5 flex-wrap justify-end">
            {/* Inheritance controls */}
            {a.supported && (
              <>
                {editPath ? (
                  <button
                    type="button"
                    onClick={() => { rescan.mutate(pathDraft); setEditPath(false) }}
                    disabled={isBusy || !pathDraft.trim()}
                    className="text-xs px-2.5 py-1 rounded-md bg-brand text-white hover:bg-brand-dark disabled:opacity-50"
                  >
                    {t('inheritance.save_rescan')}
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => rescan.mutate(undefined)}
                    disabled={isBusy}
                    className="inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-md border border-gray-200 hover:bg-gray-50 disabled:opacity-50"
                  >
                    <RefreshCw className="w-3 h-3" />
                    {rescan.isPending ? t('inheritance.scanning') : t('inheritance.rescan')}
                  </button>
                )}
                <button
                  type="button"
                  onClick={enabled ? () => disable.mutate() : () => enable.mutate()}
                  disabled={isBusy}
                  className={`inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-md disabled:opacity-50 ${
                    enabled
                      ? 'border border-gray-200 hover:bg-gray-50'
                      : 'bg-brand text-white hover:bg-brand-dark'
                  }`}
                >
                  <Power className="w-3 h-3" />
                  {enabled ? t('inheritance.disable') : t('inheritance.enable')}
                </button>
              </>
            )}

            {/* Proxy connect / disconnect */}
            {connected ? (
              <button
                onClick={() => disconnectMutation.mutate()}
                disabled={isBusy}
                className="text-xs px-2.5 py-1 rounded-md bg-red-50 text-red-600 hover:bg-red-100 disabled:opacity-50 font-medium"
              >
                {disconnectMutation.isPending ? t('agents.disconnecting') : t('agents.disconnect')}
              </button>
            ) : (
              <button
                onClick={() => {
                  if (a.id === 'openclaw') getDiff.mutate()
                  else connectMutation.mutate()
                }}
                disabled={isBusy}
                className="text-xs px-2.5 py-1 rounded-md bg-brand-light text-brand hover:bg-green-100 disabled:opacity-50 font-medium"
              >
                {getDiff.isPending ? t('agents.loading')
                  : connectMutation.isPending ? t('agents.connecting')
                  : t('agents.connect')}
              </button>
            )}
          </div>
        </div>

        {/* Post-connect hints */}
        {connectMutation.isSuccess && a.id === 'claude-code' && (
          <div className="flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <TerminalSquare size={12} className="shrink-0" />
            {t('agents.new_terminal_hint')}
          </div>
        )}
        {connectMutation.isSuccess && (a.id === 'openclaw' || a.id === 'cursor') && (
          <div className="flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <RotateCcw size={12} className="shrink-0" />
            {t('agents.restart_hint', { label: a.displayName })}
          </div>
        )}

        {/* Errors */}
        {(connectMutation.error || disconnectMutation.error) && (
          <p className="text-xs text-red-500">
            {(connectMutation.error as Error)?.message ?? (disconnectMutation.error as Error)?.message}
          </p>
        )}

        {/* Stats + logs toggle */}
        {a.status && (
          <div className="flex items-center gap-4 text-xs text-gray-500 border-t border-gray-100 pt-3">
            <div>
              <span className="font-medium text-gray-900 tabular-nums">
                {a.status.stats.requests_today}
              </span>{' '}
              {t('agents.requests_today')}
            </div>
            <div>
              <span className="font-medium text-gray-900 tabular-nums">
                ${a.status.stats.cost_today_usd.toFixed(4)}
              </span>{' '}
              {t('agents.cost')}
            </div>
            {a.status.stats.savings_today_usd > 0.000001 && (
              <div>
                <span className="font-medium text-brand tabular-nums">
                  ${a.status.stats.savings_today_usd.toFixed(4)}
                </span>{' '}
                {t('agents.saved')}
              </div>
            )}
            <button
              onClick={onToggleLogs}
              className="ml-auto flex items-center gap-1 hover:text-gray-900 transition-colors"
            >
              {logsExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
              {logsExpanded ? t('agents.hide_logs') : t('agents.show_logs')}
            </button>
          </div>
        )}
      </div>

      {/* Logs panel */}
      {logsExpanded && (
        <div className="border-t border-gray-100 bg-gray-50 px-4 py-3">
          {logsLoading ? (
            <p className="text-xs text-gray-500">{t('agents.logs_loading')}</p>
          ) : logs.length === 0 ? (
            <p className="text-xs text-gray-500">{t('agents.logs_empty')}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-gray-500 font-medium text-left">
                    <th className="pb-2 w-24">{t('agents.col_time')}</th>
                    <th className="pb-2">{t('agents.col_model')}</th>
                    <th className="pb-2">{t('agents.col_provider')}</th>
                    <th className="pb-2 text-right">{t('agents.col_tokens')}</th>
                    <th className="pb-2 text-right">{t('agents.col_cost')}</th>
                    <th className="pb-2 text-right">{t('agents.col_latency')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {logs.map((log) => (
                    <tr key={log.id} className={log.status_code >= 400 ? 'text-red-500' : 'text-gray-700'}>
                      <td className="py-1.5 pr-2 text-gray-500 tabular-nums">
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

      {/* Backups */}
      {configPath && (
        <div className="px-4 pb-3 border-t border-gray-100">
          <button
            onClick={() => setShowBackups(!showBackups)}
            className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-700 pt-3"
          >
            <History size={13} />
            {t('agents.backups')}
            {showBackups ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
          </button>
          {showBackups && (
            <div className="mt-2">
              <BackupsPanel agentName={a.id} />
            </div>
          )}
        </div>
      )}

      {/* Diff modal */}
      {showDiff && pendingDiff && (
        <DiffModal
          diff={pendingDiff}
          loading={connectMutation.isPending}
          onConfirm={() => { setShowDiff(false); connectMutation.mutate() }}
          onCancel={() => { setShowDiff(false); setPendingDiff(null) }}
        />
      )}
    </div>
  )
}

// ─── Helpers ───────────────────────────────────────────────────────────────

function relativeTime(msUTC: number): string {
  const seconds = Math.floor((Date.now() - msUTC) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}
