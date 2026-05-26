import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Link2, Link2Off, RefreshCw, ChevronDown, ChevronUp,
  TerminalSquare, RotateCcw, History, Check, Power, Edit2,
  ExternalLink,
} from 'lucide-react'
import {
  api,
  type BackupInfo, type AppDiff,
  type SupportedApp, type ConfiguredApp, type KeyHintChannel,
} from '../api/client'
import { PageHeader } from '../components/ui'

interface AppStats {
  requests_today: number
  cost_today_usd: number
  savings_today_usd: number
}

interface AppStatus {
  name: string
  config_path?: string
  cli_path?: string
  connected: boolean
  providers?: string[]
  stats: AppStats
  key_hints?: KeyHintChannel[]
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
  cached_tokens?: number
  cache_write_tokens?: number
  cost_usd: number
  latency_ms: number
  status_code: number
}

interface UnifiedApp {
  id: string
  displayName: string
  supported?: SupportedApp
  config?: ConfiguredApp
  status?: AppStatus
}

const APP_LABELS: Record<string, string> = {
  'openclaw': 'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor': 'Cursor',
  'hermes': 'Hermes',
}

// ─── DiffModal ─────────────────────────────────────────────────────────────

function DiffModal({ diff, onConfirm, onCancel, loading }: {
  diff: AppDiff
  onConfirm: () => void
  onCancel: () => void
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[80vh] flex flex-col">
        <div className="p-5 border-b border-gray-100">
          <h2 className="font-semibold text-sm">{t('apps.preview_changes')}</h2>
          <p className="text-xs text-gray-500 mt-0.5">{t('apps.preview_detail')}</p>
        </div>
        <div className="flex-1 overflow-auto p-5 grid grid-cols-2 gap-4 min-h-0">
          <div>
            <p className="text-xs text-gray-400 mb-1 font-medium">{t('apps.before')}</p>
            <pre className="text-xs bg-red-50 border border-red-100 rounded-lg p-3 overflow-auto max-h-64 whitespace-pre-wrap">{diff.before}</pre>
          </div>
          <div>
            <p className="text-xs text-gray-400 mb-1 font-medium">{t('apps.after')}</p>
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
            {loading ? t('apps.connecting') : t('apps.confirm_connect')}
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
    queryKey: ['app-backups', agentName],
    queryFn: () => api.appBackups(agentName),
  })

  const restore = useMutation({
    mutationFn: (filename: string) => api.appRestore(agentName, filename),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      qc.invalidateQueries({ queryKey: ['app-backups', agentName] })
    },
  })

  if (isLoading) return <p className="text-xs text-gray-500 py-2">{t('apps.backups_loading')}</p>
  if (backups.length === 0) return <p className="text-xs text-gray-500 py-2">{t('apps.backups_empty')}</p>

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
              if (window.confirm(t('apps.restore_confirm', { date: new Date(b.created_at).toLocaleString() }))) {
                restore.mutate(b.filename)
              }
            }}
            disabled={restore.isPending}
            className="text-blue-600 hover:text-blue-800 disabled:opacity-40 font-medium"
          >
            {t('apps.restore')}
          </button>
        </div>
      ))}
      {restore.isError && <p className="text-xs text-red-500">{t('apps.restore_failed')}</p>}
    </div>
  )
}

// ─── Page ──────────────────────────────────────────────────────────────────

export default function Apps() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [expandedLogs, setExpandedLogs] = useState<string | null>(null)

  const { data: supported = [], isLoading: loadingSupported } = useQuery<SupportedApp[]>({
    queryKey: ['apps-supported'],
    queryFn: api.appsSupported,
    staleTime: 60_000,
  })
  const { data: configured = [] } = useQuery<ConfiguredApp[]>({
    queryKey: ['apps-configured'],
    queryFn: api.appsConfigured,
    refetchInterval: 15_000,
  })
  const { data: statuses = [], isLoading: loadingStatuses, refetch } = useQuery<AppStatus[]>({
    queryKey: ['apps'],
    queryFn: () =>
      fetch('/internal/apps', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<AppStatus[]>
      }),
    refetchInterval: 30_000,
  })

  const cfgByID = new Map(configured.map((c) => [c.app_id, c]))
  const statusByName = new Map(statuses.map((a) => [a.name, a]))

  // Build unified list starting from the scanner registry (canonical order).
  // Any legacy agent that appears in /internal/apps but not in supported
  // (e.g. older daemon build) is appended at the end.
  const seenIDs = new Set<string>()
  const agents: UnifiedApp[] = []

  for (const s of supported) {
    seenIDs.add(s.app_id)
    agents.push({
      id: s.app_id,
      displayName: s.display_name,
      supported: s,
      config: cfgByID.get(s.app_id),
      status: statusByName.get(s.app_id),
    })
  }
  for (const st of statuses) {
    if (!seenIDs.has(st.name)) {
      agents.push({
        id: st.name,
        displayName: APP_LABELS[st.name] ?? st.name,
        status: st,
      })
    }
  }

  // Split connected vs available so the page leads with the agents that are
  // actually consuming tokens through krouter. The user's framing: each AI
  // agent is the first-class entity (it's what calls providers); the hosting
  // app — claude-code, openclaw, … — is configuration metadata.
  const connectedApps = agents.filter((a) => a.status?.connected)
  const availableApps = agents.filter((a) => !a.status?.connected)

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['apps'] })
    qc.invalidateQueries({ queryKey: ['apps-configured'] })
  }

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader
        title={t('apps.title')}
        right={
          <button
            onClick={() => { refetch(); invalidate() }}
            className="flex items-center gap-1.5 text-sm font-medium text-muted hover:text-ink border border-line-strong bg-card px-3 py-1.5 rounded-lg transition-colors"
          >
            <RefreshCw size={14} />
            {t('apps.re_detect')}
          </button>
        }
      />

      {(loadingSupported || loadingStatuses) ? (
        <p className="text-sm text-gray-500">{t('apps.detecting')}</p>
      ) : agents.length === 0 ? (
        <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-8 text-center space-y-1">
          <p className="text-sm text-gray-500">{t('apps.none_detected')}</p>
          <p className="text-xs text-gray-500">{t('apps.none_detail')}</p>
        </div>
      ) : (
        <div className="space-y-6">
          {connectedApps.length > 0 && (
            <section className="space-y-3">
              <SectionHeader
                label={t('apps.section_connected')}
                count={connectedApps.length}
              />
              {connectedApps.map((a) => (
                <UnifiedAgentCard
                  key={a.id}
                  agent={a}
                  logsExpanded={expandedLogs === a.id}
                  onToggleLogs={() => setExpandedLogs(expandedLogs === a.id ? null : a.id)}
                  onMutationSuccess={invalidate}
                />
              ))}
            </section>
          )}

          {availableApps.length > 0 && (
            <section className="space-y-2">
              <SectionHeader
                label={t('apps.section_available')}
                count={availableApps.length}
              />
              <p className="text-xs text-gray-500">{t('apps.section_available_hint')}</p>
              <div className="space-y-1.5">
                {availableApps.map((a) => (
                  <AvailableAgentRow
                    key={a.id}
                    agent={a}
                    onMutationSuccess={invalidate}
                  />
                ))}
              </div>
            </section>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Section header ────────────────────────────────────────────────────────

function SectionHeader({ label, count }: { label: string; count: number }) {
  return (
    <div className="flex items-baseline gap-2">
      <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-500">{label}</h2>
      <span className="text-[11px] tabular-nums text-gray-400">{count}</span>
    </div>
  )
}

// ─── ProviderChip ──────────────────────────────────────────────────────────
//
// Wherever the Agents page surfaces a specific provider name, render it as
// a chip that deep-links to /providers#provider-<name>. The Providers page
// reads location.hash and auto-expands the matching card.

function ProviderChip({ name }: { name: string }) {
  return (
    <Link
      to={`/providers#provider-${encodeURIComponent(name)}`}
      className="inline-flex items-center gap-1 text-[11px] font-medium px-1.5 py-0.5 rounded border border-gray-200 bg-white text-gray-700 hover:border-brand hover:text-brand-ink transition-colors"
    >
      {name}
      <ExternalLink size={9} className="opacity-50" />
    </Link>
  )
}

// ─── UnifiedAgentCard ──────────────────────────────────────────────────────

function UnifiedAgentCard({
  agent: a,
  logsExpanded,
  onToggleLogs,
  onMutationSuccess,
}: {
  agent: UnifiedApp
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
  const [pendingDiff, setPendingDiff] = useState<AppDiff | null>(null)
  // Post-connect restart hint, driven by the backend's restart_kind so every
  // agent (incl. Hermes) is covered and we never guess from the agent id.
  const [restartKind, setRestartKind] = useState<string | null>(null)

  // Inheritance mutations
  const enable = useMutation({
    mutationFn: () => api.appEnable(a.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['apps-configured'] }) },
  })
  const disable = useMutation({
    mutationFn: () => api.appDisable(a.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['apps-configured'] }) },
  })
  const rescan = useMutation({
    mutationFn: (path?: string) => api.appRescan(a.id, path),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['apps-configured'] }) },
  })

  // Proxy connect/disconnect mutations
  const connectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/apps/${a.id}/connect`, { method: 'POST', credentials: 'include' })
        .then((r) => {
          if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
          return r.json() as Promise<{ restart_kind?: string }>
        }),
    onSuccess: (data) => {
      setRestartKind(data?.restart_kind ?? 'process')
      onMutationSuccess()
    },
  })
  const disconnectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/apps/${a.id}/disconnect`, { method: 'POST', credentials: 'include' })
        .then((r) => {
          if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
          return r.json()
        }),
    onSuccess: () => {
      setRestartKind(null)
      onMutationSuccess()
    },
  })
  const getDiff = useMutation({
    mutationFn: () => api.appDiff(a.id),
    onSuccess: (diff) => { setPendingDiff(diff); setShowDiff(true) },
  })

  const isBusy =
    enable.isPending || disable.isPending || rescan.isPending ||
    connectMutation.isPending || disconnectMutation.isPending || getDiff.isPending

  const { data: logs = [], isLoading: logsLoading } = useQuery<LogRow[]>({
    queryKey: ['app-logs', a.id],
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
                  <Link2 className="w-3 h-3" /> {t('apps.connected')}
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-[11px] font-medium text-gray-400 bg-gray-50 rounded-full px-2 py-0.5">
                  <Link2Off className="w-3 h-3" /> {t('apps.not_connected')}
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
                {disconnectMutation.isPending ? t('apps.disconnecting') : t('apps.disconnect')}
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
                {getDiff.isPending ? t('apps.loading')
                  : connectMutation.isPending ? t('apps.connecting')
                  : t('apps.connect')}
              </button>
            )}
          </div>
        </div>

        {/* Post-connect hint — krouter never restarts the app itself, it only
            tells the user. restart_kind comes from the connect response:
            "shell" = re-read env on a new terminal (Claude Code), otherwise
            restart the app process. */}
        {restartKind === 'shell' && (
          <div className="flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <TerminalSquare size={12} className="shrink-0" />
            {t('apps.new_terminal_hint')}
          </div>
        )}
        {restartKind && restartKind !== 'shell' && (
          <div className="flex items-center gap-2 text-xs text-gray-500 bg-gray-50 rounded-lg px-3 py-2">
            <RotateCcw size={12} className="shrink-0" />
            {t('apps.restart_hint', { label: a.displayName })}
          </div>
        )}

        {/* Errors */}
        {(connectMutation.error || disconnectMutation.error) && (
          <p className="text-xs text-red-500">
            {(connectMutation.error as Error)?.message ?? (disconnectMutation.error as Error)?.message}
          </p>
        )}

        {/* Inherited provider chips */}
        {a.status && (a.status.providers ?? []).length > 0 && (
          <div className="flex items-center gap-1.5 flex-wrap border-t border-gray-100 pt-3">
            <span className="text-[11px] uppercase tracking-wider text-gray-500 font-semibold mr-1">
              {t('apps.providers_label')}
            </span>
            {(a.status.providers ?? []).map((name) => (
              <ProviderChip key={name} name={name} />
            ))}
          </div>
        )}

        {/* Stats + logs toggle */}
        {a.status && (
          <div className="flex items-center gap-4 text-xs text-gray-500 border-t border-gray-100 pt-3">
            <div>
              <span className="font-medium text-gray-900 tabular-nums">
                {a.status.stats.requests_today}
              </span>{' '}
              {t('apps.requests_today')}
            </div>
            <div>
              <span className="font-medium text-gray-900 tabular-nums">
                ${a.status.stats.cost_today_usd.toFixed(4)}
              </span>{' '}
              {t('apps.cost')}
            </div>
            {a.status.stats.savings_today_usd > 0.000001 && (
              <div>
                <span className="font-medium text-brand tabular-nums">
                  ${a.status.stats.savings_today_usd.toFixed(4)}
                </span>{' '}
                {t('apps.saved')}
              </div>
            )}
            <button
              onClick={onToggleLogs}
              className="ml-auto flex items-center gap-1 hover:text-gray-900 transition-colors"
            >
              {logsExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
              {logsExpanded ? t('apps.hide_logs') : t('apps.show_logs')}
            </button>
          </div>
        )}

        {/* Key-hint channels */}
        {a.status?.key_hints && a.status.key_hints.length > 1 && (
          <KeyHintChannels hints={a.status.key_hints} />
        )}
      </div>

      {/* Logs panel */}
      {logsExpanded && (
        <div className="border-t border-gray-100 bg-gray-50 px-4 py-3">
          {logsLoading ? (
            <p className="text-xs text-gray-500">{t('apps.logs_loading')}</p>
          ) : logs.length === 0 ? (
            <p className="text-xs text-gray-500">{t('apps.logs_empty')}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-gray-500 font-medium text-left">
                    <th className="pb-2 w-24">{t('apps.col_time')}</th>
                    <th className="pb-2">{t('apps.col_model')}</th>
                    <th className="pb-2">{t('apps.col_provider')}</th>
                    <th className="pb-2 text-right">{t('apps.col_tokens')}</th>
                    <th className="pb-2 text-right">{t('apps.col_cost')}</th>
                    <th className="pb-2 text-right">{t('apps.col_latency')}</th>
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
                        {(log.input_tokens + log.output_tokens + (log.cached_tokens ?? 0) + (log.cache_write_tokens ?? 0)).toLocaleString()}
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
            {t('apps.backups')}
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

// ─── AvailableAgentRow ─────────────────────────────────────────────────────
//
// Compact row for agents detected on disk but not currently using krouter.
// One-line summary — name, detected path, single Connect button. Anything
// more elaborate would crowd the section, which is meant to recede behind
// the actively-connected agents at the top of the page.

function AvailableAgentRow({
  agent: a,
  onMutationSuccess,
}: {
  agent: UnifiedApp
  onMutationSuccess: () => void
}) {
  const { t } = useTranslation()
  const [pendingDiff, setPendingDiff] = useState<AppDiff | null>(null)
  const [showDiff, setShowDiff] = useState(false)

  const connectMutation = useMutation({
    mutationFn: () =>
      fetch(`/internal/apps/${a.id}/connect`, { method: 'POST', credentials: 'include' })
        .then((r) => {
          if (!r.ok) return r.json().then((e: { error: string }) => { throw new Error(e.error) })
          return r.json()
        }),
    onSuccess: onMutationSuccess,
  })
  const getDiff = useMutation({
    mutationFn: () => api.appDiff(a.id),
    onSuccess: (diff) => { setPendingDiff(diff); setShowDiff(true) },
  })

  const configPath = a.config?.config_path ?? a.supported?.default_path ?? a.status?.config_path
  const detected = !!a.status   // appears in /internal/apps — binary on PATH
  const isBusy = connectMutation.isPending || getDiff.isPending

  return (
    <div className="flex items-center gap-3 px-3 py-2 bg-white rounded-lg border border-gray-200">
      <span className="shrink-0 inline-flex h-7 w-7 items-center justify-center rounded-full bg-gray-200 text-gray-500 text-[10px] font-bold">
        {a.id.slice(0, 2).toUpperCase()}
      </span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-medium text-sm text-gray-900">{a.displayName}</span>
          {!detected && (
            <span className="text-[10px] uppercase tracking-wider text-gray-400 border border-gray-200 rounded px-1.5 py-0.5">
              {t('apps.not_installed')}
            </span>
          )}
        </div>
        {configPath && (
          <p className="text-[11px] text-gray-400 font-mono truncate" title={configPath}>
            {configPath}
          </p>
        )}
      </div>
      {detected && (
        <button
          onClick={() => {
            if (a.id === 'openclaw') getDiff.mutate()
            else connectMutation.mutate()
          }}
          disabled={isBusy}
          className="shrink-0 text-xs px-2.5 py-1 rounded-md bg-brand-light text-brand hover:bg-green-100 disabled:opacity-50 font-medium"
        >
          {getDiff.isPending ? t('apps.loading')
            : connectMutation.isPending ? t('apps.connecting')
            : t('apps.connect')}
        </button>
      )}
      {connectMutation.error && (
        <span className="text-[11px] text-red-500 ml-2">
          {(connectMutation.error as Error).message}
        </span>
      )}
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

// ─── Key-hint channel list ─────────────────────────────────────────────────

function KeyHintChannels({ hints }: { hints: KeyHintChannel[] }) {
  const { t } = useTranslation()
  return (
    <div className="border-t border-gray-100 pt-3 mt-1">
      <p className="text-[10px] uppercase tracking-wide text-gray-400 font-semibold mb-1.5">
        {t('apps.key_hint_channels')}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {hints.map((h) => (
          <span
            key={h.hint || '__nokey__'}
            className="inline-flex items-center gap-1.5 text-[11px] font-mono px-2 py-0.5 bg-gray-100 rounded text-gray-600"
          >
            <span className="text-gray-400">···</span>
            {h.hint || t('apps.key_hint_unknown')}
            <span className="text-gray-400 font-sans">·</span>
            <span className="tabular-nums">{h.requests}</span>
          </span>
        ))}
      </div>
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
