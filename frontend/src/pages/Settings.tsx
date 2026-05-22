import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Settings as ISettings, type Preset, type PricingStatus } from '../api/client'

const PRESETS: Preset[] = ['saver', 'balanced', 'quality']

const NOTIFICATION_CATEGORIES = [
  { key: 'quota_warning', label: 'Quota Warnings' },
  { key: 'announcement_new', label: 'New Announcements' },
  { key: 'upgrade_available', label: 'Updates Available' },
  { key: 'free_credit', label: 'Free Credits' },
  { key: 'provider_news', label: 'Provider News' },
  { key: 'tip', label: 'Tips & Suggestions' },
  { key: 'critical_warning', label: 'Critical Warnings' },
]

const BUDGET_THRESHOLDS = [
  { key: 'daily', label: 'Daily limit (USD)' },
  { key: 'weekly', label: 'Weekly limit (USD)' },
]

function fmtUSD(n: number) {
  return n < 0.001 ? '<$0.001' : `$${n.toFixed(3)}`
}

function fmtMTok(n: number) {
  if (!n) return '—'
  return `$${n.toFixed(2)}/M`
}

function fmtSyncTime(iso: string) {
  if (!iso) return 'Never'
  const d = new Date(iso)
  return d.toLocaleString()
}

export default function Settings() {
  const qc = useQueryClient()
  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
  })

  const { data: pricing } = useQuery<PricingStatus>({
    queryKey: ['pricingStatus'],
    queryFn: api.pricingStatus,
    refetchInterval: 60_000,
  })

  const save = useMutation({
    mutationFn: (patch: Partial<ISettings>) => api.patchSettings(patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  const [exportFrom, setExportFrom] = useState('')
  const [exportTo, setExportTo] = useState('')

  const resetData = useMutation({
    mutationFn: api.resetData,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['logs'] }),
  })

  const uninstall = useMutation({
    mutationFn: api.uninstall,
  })

  if (isLoading || !settings) return <div className="p-6 text-sm text-gray-400">Loading…</div>

  return (
    <div className="p-6 space-y-6 max-w-xl mx-auto">
      <h1 className="text-lg font-semibold">Settings</h1>

      {/* Preset */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">Routing Preset</h2>
        <div className="flex gap-2">
          {PRESETS.map((p) => (
            <button
              key={p}
              onClick={() => save.mutate({ preset: p })}
              className={[
                'flex-1 rounded-lg border px-3 py-2 text-sm capitalize transition-colors',
                settings.preset === p
                  ? 'border-brand bg-brand-light text-brand'
                  : 'border-border hover:border-brand/50',
              ].join(' ')}
            >
              {p}
            </button>
          ))}
        </div>
      </section>

      {/* Language */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">Language</h2>
        <select
          value={settings.language}
          onChange={(e) => save.mutate({ language: e.target.value })}
          className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
        >
          <option value="en">English</option>
          <option value="zh-CN">中文</option>
        </select>
      </section>

      {/* Desktop notifications */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">Desktop Notifications</h2>
        <div className="space-y-2">
          {NOTIFICATION_CATEGORIES.map(({ key, label }) => (
            <label key={key} className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={settings.notification_categories?.[key] ?? true}
                onChange={(e) =>
                  save.mutate({
                    notification_categories: {
                      ...(settings.notification_categories ?? {}),
                      [key]: e.target.checked,
                    },
                  })
                }
                className="w-4 h-4 rounded accent-blue-500"
              />
              <span className="text-sm">{label}</span>
            </label>
          ))}
        </div>
      </section>

      {/* Budget limits */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">Budget Limits</h2>
        <p className="text-xs text-gray-500">Requests are blocked when the limit is reached. Set to 0 to disable. Currency is always USD.</p>
        <div className="space-y-3">
          {BUDGET_THRESHOLDS.map(({ key, label }) => (
            <div key={key} className="flex items-center gap-3">
              <label className="text-sm w-36">{label}</label>
              <input
                type="number"
                min={0}
                step={0.5}
                defaultValue={settings.budget_warnings?.[key] ?? 0}
                onBlur={(e) => {
                  const v = parseFloat(e.target.value)
                  if (!isNaN(v)) {
                    save.mutate({
                      budget_warnings: { ...(settings.budget_warnings ?? {}), [key]: v },
                    })
                  }
                }}
                className="w-28 border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
              />
            </div>
          ))}
        </div>
      </section>

      {/* Pricing */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-medium">Pricing Data</h2>
          {pricing && (
            <span className={[
              'text-xs px-2 py-0.5 rounded-full font-medium',
              pricing.source === 'live' ? 'bg-green-100 text-green-700' :
              pricing.source === 'cache' ? 'bg-yellow-100 text-yellow-700' :
              'bg-gray-100 text-gray-500',
            ].join(' ')}>
              {pricing.source}
            </span>
          )}
        </div>

        {pricing ? (
          <>
            <div className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
              <span className="text-gray-500">Last synced</span>
              <span className="font-mono text-xs">{fmtSyncTime(pricing.last_sync_at)}</span>
              <span className="text-gray-500">Models tracked</span>
              <span>{pricing.model_count.toLocaleString()}</span>
              <span className="text-gray-500">Cost this month</span>
              <span>{fmtUSD(pricing.cost_this_month_usd)}</span>
              <span className="text-gray-500">Saved this month</span>
              <span className="text-green-600 font-medium">{fmtUSD(pricing.saved_this_month_usd)}</span>
            </div>

            {pricing.top_models.length > 0 && (
              <div className="space-y-1">
                <p className="text-xs text-gray-400 uppercase tracking-wide">Top models (30 days)</p>
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-100">
                      <th className="text-left py-1 font-normal">Model</th>
                      <th className="text-right py-1 font-normal">Reqs</th>
                      <th className="text-right py-1 font-normal">Cost</th>
                      <th className="text-right py-1 font-normal">In/M</th>
                      <th className="text-right py-1 font-normal">Out/M</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pricing.top_models.map((m) => (
                      <tr key={`${m.provider}/${m.model}`} className="border-b border-gray-50">
                        <td className="py-1 text-gray-700 max-w-[140px] truncate" title={m.model}>{m.model}</td>
                        <td className="py-1 text-right tabular-nums">{m.requests}</td>
                        <td className="py-1 text-right tabular-nums">{fmtUSD(m.cost_usd)}</td>
                        <td className="py-1 text-right tabular-nums">{fmtMTok(m.input_per_mtok)}</td>
                        <td className="py-1 text-right tabular-nums">{fmtMTok(m.output_per_mtok)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        ) : (
          <p className="text-sm text-gray-400">Loading…</p>
        )}
      </section>

      {/* Data Management */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
        <h2 className="text-sm font-medium">Data Management</h2>

        {/* Export logs */}
        <div className="space-y-2">
          <p className="text-xs text-gray-500">Export request logs as CSV by date range.</p>
          <div className="flex items-center gap-2 flex-wrap">
            <input
              type="date"
              value={exportFrom}
              onChange={(e) => setExportFrom(e.target.value)}
              className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
            />
            <span className="text-sm text-gray-400">to</span>
            <input
              type="date"
              value={exportTo}
              onChange={(e) => setExportTo(e.target.value)}
              className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
            />
            <a
              href={exportFrom && exportTo ? api.logsExportUrl(exportFrom, exportTo) : '#'}
              download
              onClick={(e) => { if (!exportFrom || !exportTo) e.preventDefault() }}
              className={[
                'flex items-center gap-1.5 text-sm border rounded-lg px-3 py-1.5',
                exportFrom && exportTo
                  ? 'border-gray-200 text-gray-600 hover:text-gray-900 hover:border-gray-400'
                  : 'border-gray-100 text-gray-300 cursor-not-allowed',
              ].join(' ')}
            >
              Export CSV
            </a>
          </div>
        </div>

        {/* Reset data */}
        <div className="flex items-center justify-between pt-2 border-t border-gray-50">
          <div>
            <p className="text-sm font-medium text-gray-700">Reset All Data</p>
            <p className="text-xs text-gray-400">Delete all request history from the local database.</p>
          </div>
          <button
            onClick={() => {
              if (window.confirm('Delete all request history? This cannot be undone.')) {
                resetData.mutate()
              }
            }}
            disabled={resetData.isPending}
            className="text-sm text-red-600 border border-red-200 rounded-lg px-3 py-1.5 hover:bg-red-50 disabled:opacity-40"
          >
            {resetData.isPending ? 'Resetting…' : 'Reset Data'}
          </button>
        </div>

        {/* Uninstall */}
        <div className="flex items-center justify-between pt-2 border-t border-gray-50">
          <div>
            <p className="text-sm font-medium text-gray-700">Uninstall</p>
            <p className="text-xs text-gray-400">Disconnect all agents. Run <code className="font-mono text-xs">krouter uninstall</code> to remove service files.</p>
          </div>
          <button
            onClick={() => {
              if (window.confirm('Disconnect all agents? You can reconnect them later.')) {
                uninstall.mutate()
              }
            }}
            disabled={uninstall.isPending}
            className="text-sm text-red-600 border border-red-200 rounded-lg px-3 py-1.5 hover:bg-red-50 disabled:opacity-40"
          >
            {uninstall.isPending ? 'Disconnecting…' : 'Uninstall'}
          </button>
        </div>
        {(resetData.isError || uninstall.isError) && (
          <p className="text-sm text-red-500">Operation failed. Please try again.</p>
        )}
      </section>

      {save.isError && (
        <p className="text-sm text-red-500">Failed to save settings. Please try again.</p>
      )}
    </div>
  )
}
