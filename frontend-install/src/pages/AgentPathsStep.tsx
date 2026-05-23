import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  api,
  type SupportedAgent,
  type PreviewResult,
} from '../api/client'

interface Props {
  onNext: () => void
}

// Per-row mutable state. Kept in the parent component so the user can see
// every agent's status (count / error) at a glance before committing.
interface RowState {
  enabled: boolean
  path: string
  preview?: PreviewResult
  previewLoading: boolean
}

// AgentPathsStep is spec/04 §4's "Agent Paths" step. It lets the user pick
// which AI agents on disk the daemon should inherit from. Each row shows the
// default config path (user-editable), a Preview button that runs the Scanner
// without persisting anything, and a checkbox to opt-in. The Save action
// writes pending-agents.json; the daemon reads it on startup.
//
// Following the "no skip" rule from the design discussion: the user MUST
// select at least one agent to continue. Wizard installs are pointless without
// at least one upstream vendor.
export default function AgentPathsStep({ onNext }: Props) {
  const { t } = useTranslation()
  const [supported, setSupported] = useState<SupportedAgent[] | null>(null)
  const [rows, setRows] = useState<Record<string, RowState>>({})
  const [saving, setSaving] = useState(false)
  const [loadErr, setLoadErr] = useState('')

  // Initial fetch: supported list comes from the installer binary's Scanner
  // registry. Pre-populate per-row state with default paths.
  useEffect(() => {
    api.agentsSupported()
      .then((list) => {
        setSupported(list)
        const init: Record<string, RowState> = {}
        for (const s of list) {
          init[s.agent_id] = { enabled: false, path: s.default_path, previewLoading: false }
        }
        setRows(init)
      })
      .catch((e: Error) => setLoadErr(e.message))
  }, [])

  const updateRow = (id: string, patch: Partial<RowState>) => {
    setRows((prev) => ({ ...prev, [id]: { ...prev[id], ...patch } }))
  }

  const runPreview = async (id: string) => {
    const row = rows[id]
    if (!row) return
    updateRow(id, { previewLoading: true })
    try {
      const result = await api.agentsPreview(id, row.path)
      updateRow(id, { preview: result, previewLoading: false })
    } catch (e) {
      updateRow(id, {
        preview: { endpoints: [], error: (e as Error).message },
        previewLoading: false,
      })
    }
  }

  const enabledCount = Object.values(rows).filter((r) => r.enabled).length

  const handleSave = async () => {
    setSaving(true)
    try {
      await api.agentsSelect(
        Object.entries(rows).map(([agent_id, r]) => ({
          agent_id,
          enabled: r.enabled,
          config_path: r.path,
        })),
      )
      onNext()
    } catch (e) {
      setLoadErr((e as Error).message)
      setSaving(false)
    }
  }

  if (loadErr) {
    return (
      <div>
        <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">{t('agentPaths.title')}</h2>
        <p className="text-red-500 text-sm">{loadErr}</p>
      </div>
    )
  }
  if (supported === null) {
    return (
      <div>
        <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">{t('agentPaths.title')}</h2>
        <p className="text-gray-400 text-sm animate-pulse">{t('agentPaths.loading')}</p>
      </div>
    )
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">{t('agentPaths.title')}</h2>
      <p className="text-sm text-gray-500 mb-6">
        {t('agentPaths.detail')}
      </p>

      <ul className="divide-y divide-border mb-6">
        {supported.map((s) => {
          const row = rows[s.agent_id]
          if (!row) return null
          return (
            <li key={s.agent_id} className="py-3">
              <label className="flex items-start gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={row.enabled}
                  onChange={(e) => updateRow(s.agent_id, { enabled: e.target.checked })}
                  className="mt-1"
                />
                <div className="flex-1 min-w-0">
                  <p className="font-medium text-gray-800">{s.display_name}</p>

                  <div className="mt-1 flex items-center gap-2">
                    <input
                      type="text"
                      value={row.path}
                      onChange={(e) => updateRow(s.agent_id, { path: e.target.value, preview: undefined })}
                      className="flex-1 px-2 py-1 border border-border rounded text-xs font-mono"
                    />
                    <button
                      type="button"
                      onClick={() => runPreview(s.agent_id)}
                      disabled={row.previewLoading}
                      className="text-xs px-2.5 py-1 rounded border border-border hover:bg-surface disabled:opacity-50"
                    >
                      {row.previewLoading ? t('agentPaths.scanning') : t('agentPaths.preview')}
                    </button>
                  </div>

                  {row.preview && row.preview.error && (
                    <p className="text-xs text-red-500 mt-1">{row.preview.error}</p>
                  )}
                  {row.preview && !row.preview.error && (
                    <p className="text-xs text-gray-500 mt-1">
                      {row.preview.endpoints.length === 0
                        ? t('agentPaths.no_vendors')
                        : `${t('agentPaths.vendors_found', { count: row.preview.endpoints.length })}: ${row.preview.endpoints.map((e) => e.provider).join(', ')}`}
                    </p>
                  )}
                </div>
              </label>
            </li>
          )
        })}
      </ul>

      <button
        onClick={handleSave}
        disabled={saving || enabledCount === 0}
        className="w-full bg-brand hover:bg-brand-dark disabled:opacity-40 disabled:cursor-not-allowed text-white font-semibold py-2.5 px-4 rounded-xl transition-colors"
      >
        {saving ? t('agentPaths.saving') : enabledCount === 0
          ? t('agentPaths.select_one')
          : t('agentPaths.continue', { count: enabledCount })}
      </button>
    </div>
  )
}
