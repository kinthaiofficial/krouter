import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, ChevronDown, ChevronUp, Check, Power, Edit2 } from 'lucide-react'
import {
  api,
  type SupportedAgent,
  type ConfiguredAgent,
} from '../api/client'

// AppInheritanceSection renders the spec/04 app-inheritance UI: it lists
// every Scanner the daemon binary supports, joins with the user's
// agent_settings rows, and surfaces the inherited_endpoints count, last scan
// timestamp, and inline controls for Enable / Disable / Rescan / edit path.
//
// This component is the source of truth for which agents krouter is treating
// as configured. The legacy v2.0.47 connect/disconnect UI in pages/Agents.tsx
// stays for now as a fallback during transition.
export default function AppInheritanceSection() {
  const { t } = useTranslation()
  const qc = useQueryClient()

  const { data: supported, isLoading: loadingSupported } = useQuery({
    queryKey: ['apps-supported'],
    queryFn: api.appsSupported,
    staleTime: 60_000,
  })
  const { data: configured } = useQuery({
    queryKey: ['apps-configured'],
    queryFn: api.appsConfigured,
    refetchInterval: 15_000,
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['apps-configured'] })
  }

  const enable = useMutation({
    mutationFn: (id: string) => api.appEnable(id),
    onSuccess: invalidate,
  })
  const disable = useMutation({
    mutationFn: (id: string) => api.appDisable(id),
    onSuccess: invalidate,
  })
  const rescan = useMutation({
    mutationFn: ({ id, path }: { id: string; path?: string }) => api.appRescan(id, path),
    onSuccess: invalidate,
  })

  if (loadingSupported) {
    return <SectionShell><p className="text-xs text-gray-400">{t('agents.loading')}</p></SectionShell>
  }
  if (!supported || supported.length === 0) {
    return null
  }

  // Join supported registry with configured rows. supported is the canonical
  // list (Scanner registry compiled into the daemon); configured fills in the
  // user-state. Missing config rows mean "user hasn't touched this agent
  // yet".
  const cfgByID = new Map((configured ?? []).map((c) => [c.app_id, c]))

  return (
    <SectionShell>
      <header className="flex items-baseline justify-between mb-3">
        <h2 className="text-sm font-semibold">{t('inheritance.title')}</h2>
        <span className="text-xs text-gray-400">
          {t('inheritance.summary', { known: supported.length, enabled: (configured ?? []).filter((c) => c.enabled).length })}
        </span>
      </header>
      <ul className="divide-y divide-gray-100">
        {supported.map((s) => (
          <AgentRow
            key={s.app_id}
            supported={s}
            config={cfgByID.get(s.app_id)}
            busy={
              enable.isPending || disable.isPending || rescan.isPending
            }
            onEnable={() => enable.mutate(s.app_id)}
            onDisable={() => disable.mutate(s.app_id)}
            onRescan={(path?: string) => rescan.mutate({ id: s.app_id, path })}
          />
        ))}
      </ul>
    </SectionShell>
  )
}

function SectionShell({ children }: { children: React.ReactNode }) {
  return (
    <section
      data-testid="app-inheritance-section"
      className="bg-white border border-border rounded-2xl p-5 shadow-sm"
    >
      {children}
    </section>
  )
}

function AgentRow({
  supported,
  config,
  busy,
  onEnable,
  onDisable,
  onRescan,
}: {
  supported: SupportedAgent
  config?: ConfiguredAgent
  busy: boolean
  onEnable: () => void
  onDisable: () => void
  onRescan: (path?: string) => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const [editPath, setEditPath] = useState(false)
  const [pathDraft, setPathDraft] = useState(config?.config_path ?? supported.default_path)
  const enabled = config?.enabled ?? false
  const inherited = config?.inherited_count ?? 0
  const lastError = config?.last_error
  const lastScannedAt = config?.last_scanned_at

  return (
    <li className="py-3">
      <div className="flex items-center gap-3">
        <span
          className={`inline-flex h-7 w-7 items-center justify-center rounded-full text-white text-[10px] font-bold tracking-tight ${enabled ? 'bg-brand' : 'bg-gray-300'}`}
          aria-hidden
        >
          {supported.app_id.slice(0, 2).toUpperCase()}
        </span>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium truncate">{supported.display_name}</p>
            {enabled && (
              <span className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald-700 bg-emerald-50 rounded-full px-2 py-0.5">
                <Check className="w-3 h-3" /> {t('common.enabled')}
              </span>
            )}
            {!enabled && config && (
              <span className="text-[11px] font-medium text-gray-500 bg-gray-100 rounded-full px-2 py-0.5">
                {t('common.disabled')}
              </span>
            )}
            {!config && (
              <span className="text-[11px] font-medium text-gray-400">{t('inheritance.not_configured')}</span>
            )}
          </div>
          <p className="text-[11px] text-gray-400 truncate">
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
          {lastError && (
            <p className="text-[11px] text-red-600 mt-0.5 truncate" title={lastError}>
              {t('common.error')}: {lastError}
            </p>
          )}
        </div>

        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => onRescan(editPath ? pathDraft : undefined)}
            disabled={busy}
            className="inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-md border border-gray-200 hover:bg-gray-50 disabled:opacity-50"
            aria-label={t('inheritance.rescan')}
          >
            <RefreshCw className="w-3.5 h-3.5" /> {t('inheritance.rescan')}
          </button>
          <button
            type="button"
            onClick={enabled ? onDisable : onEnable}
            disabled={busy}
            className={`inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-md disabled:opacity-50 ${enabled ? 'border border-gray-200 hover:bg-gray-50' : 'bg-brand text-white hover:bg-brand-strong'}`}
            aria-label={enabled ? t('inheritance.disable') : t('inheritance.enable')}
          >
            <Power className="w-3.5 h-3.5" /> {enabled ? t('inheritance.disable') : t('inheritance.enable')}
          </button>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-gray-400 hover:text-gray-700"
            aria-label={expanded ? 'Collapse' : 'Expand'}
          >
            {expanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {expanded && (
        <div className="mt-3 ml-10 space-y-2">
          <div className="flex items-center gap-2 text-xs">
            <span className="text-gray-400 w-20 shrink-0">{t('inheritance.config_path')}</span>
            {editPath ? (
              <input
                type="text"
                value={pathDraft}
                onChange={(e) => setPathDraft(e.target.value)}
                className="flex-1 px-2 py-1 border border-gray-200 rounded text-[12px] font-mono"
              />
            ) : (
              <code className="flex-1 text-[12px] text-gray-700 break-all">
                {config?.config_path ?? supported.default_path}
              </code>
            )}
            <button
              type="button"
              onClick={() => setEditPath((v) => !v)}
              className="text-gray-400 hover:text-gray-700"
              aria-label={editPath ? 'Cancel' : 'Edit path'}
            >
              <Edit2 className="w-3.5 h-3.5" />
            </button>
          </div>
          {editPath && (
            <button
              type="button"
              onClick={() => {
                onRescan(pathDraft)
                setEditPath(false)
              }}
              disabled={busy || !pathDraft.trim()}
              className="text-xs px-2.5 py-1 rounded-md bg-brand text-white hover:bg-brand-strong disabled:opacity-50"
            >
              {t('inheritance.save_rescan')}
            </button>
          )}
          <p className="text-[11px] text-gray-400">
            {t('common.default')}: <code>{supported.default_path}</code>
          </p>
        </div>
      )}
    </li>
  )
}

// relativeTime formats milliseconds-since-epoch as a coarse "5m ago" string.
// We keep it inline so the component file has no UI-helper dependency.
function relativeTime(msUTC: number): string {
  const seconds = Math.floor((Date.now() - msUTC) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
