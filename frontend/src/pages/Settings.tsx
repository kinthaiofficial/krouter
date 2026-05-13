import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Settings as ISettings, type Preset } from '../api/client'

const PRESETS: Preset[] = ['saver', 'balanced', 'quality']

const NOTIFICATION_CATEGORIES = [
  { key: 'quota_warning', label: 'Quota Warnings' },
  { key: 'announcement_new', label: 'New Announcements' },
  { key: 'upgrade_available', label: 'Updates Available' },
]

const BUDGET_THRESHOLDS = [
  { key: 'daily', label: 'Daily limit ($)' },
  { key: 'weekly', label: 'Weekly limit ($)' },
]

export default function Settings() {
  const qc = useQueryClient()
  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
  })

  const save = useMutation({
    mutationFn: (patch: Partial<ISettings>) => api.patchSettings(patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  if (isLoading || !settings) return <div className="p-6 text-sm text-gray-400">Loading…</div>

  return (
    <div className="p-6 space-y-6 max-w-xl mx-auto">
      <h1 className="text-lg font-semibold">Settings</h1>

      {/* Preset */}
      <section className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-3">
        <h2 className="text-sm font-medium">Routing Preset</h2>
        <div className="flex gap-2">
          {PRESETS.map((p) => (
            <button
              key={p}
              onClick={() => save.mutate({ preset: p })}
              className={[
                'flex-1 rounded-lg border px-3 py-2 text-sm capitalize transition-colors',
                settings.preset === p
                  ? 'border-blue-500 bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300'
                  : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500',
              ].join(' ')}
            >
              {p}
            </button>
          ))}
        </div>
      </section>

      {/* Language */}
      <section className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-3">
        <h2 className="text-sm font-medium">Language</h2>
        <select
          value={settings.language}
          onChange={(e) => save.mutate({ language: e.target.value })}
          className="border border-gray-200 dark:border-gray-600 rounded-lg px-3 py-1.5 text-sm bg-white dark:bg-gray-800"
        >
          <option value="en">English</option>
          <option value="zh-CN">中文</option>
        </select>
      </section>

      {/* Desktop notifications */}
      <section className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-3">
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

      {/* Budget warnings */}
      <section className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 space-y-3">
        <h2 className="text-sm font-medium">Budget Warnings</h2>
        <p className="text-xs text-gray-500">Set to 0 to disable.</p>
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
                className="w-28 border border-gray-200 dark:border-gray-600 rounded-lg px-3 py-1.5 text-sm bg-white dark:bg-gray-800"
              />
            </div>
          ))}
        </div>
      </section>

      {save.isError && (
        <p className="text-sm text-red-500">Failed to save settings. Please try again.</p>
      )}
    </div>
  )
}
