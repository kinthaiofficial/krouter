import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Settings as ISettings, type Preset, type PricingStatus } from '../api/client'
import i18n, { storeLang, settingsLangToI18n } from '../i18n'

const PRESETS: Preset[] = ['saver', 'balanced', 'quality']

const NOTIFICATION_CATEGORIES = [
  { key: 'quota_warning', labelKey: 'settings.notif_quota_warning' },
  { key: 'announcement_new', labelKey: 'settings.notif_announcement' },
  { key: 'upgrade_available', labelKey: 'settings.notif_upgrade' },
  { key: 'free_credit', labelKey: 'settings.notif_free_credit' },
  { key: 'provider_news', labelKey: 'settings.notif_provider_news' },
  { key: 'tip', labelKey: 'settings.notif_tip' },
  { key: 'critical_warning', labelKey: 'settings.notif_critical' },
]

const BUDGET_THRESHOLDS = [
  { key: 'daily', labelKey: 'settings.budget_daily' },
  { key: 'weekly', labelKey: 'settings.budget_weekly' },
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
  const { t } = useTranslation()
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

  // Sync settings.language to i18n on first load
  useEffect(() => {
    if (settings?.language) {
      const lang = settingsLangToI18n(settings.language)
      storeLang(lang)
      i18n.changeLanguage(lang)
    }
  }, [settings?.language])

  if (isLoading || !settings) return <div className="p-6 text-sm text-gray-400">{t('common.loading')}</div>

  return (
    <div className="p-6 space-y-6 max-w-xl mx-auto">
      <h1 className="text-lg font-semibold">{t('settings.title')}</h1>

      {/* Preset */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">{t('settings.routing_preset')}</h2>
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
        <h2 className="text-sm font-medium">{t('settings.language')}</h2>
        <select
          value={settings.language}
          onChange={(e) => {
            const newLang = e.target.value
            const i18nLang = settingsLangToI18n(newLang)
            storeLang(i18nLang)
            i18n.changeLanguage(i18nLang)
            save.mutate({ language: newLang })
          }}
          className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
        >
          <option value="en">{t('settings.lang_en')}</option>
          <option value="zh-CN">{t('settings.lang_zh')}</option>
        </select>
      </section>

      {/* Desktop notifications */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">{t('settings.notifications')}</h2>
        <div className="space-y-2">
          {NOTIFICATION_CATEGORIES.map(({ key, labelKey }) => (
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
              <span className="text-sm">{t(labelKey)}</span>
            </label>
          ))}
        </div>
      </section>

      {/* Budget limits */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium">{t('settings.budget_limits')}</h2>
        <p className="text-xs text-gray-500">{t('settings.budget_detail')}</p>
        <div className="space-y-3">
          {BUDGET_THRESHOLDS.map(({ key, labelKey }) => (
            <div key={key} className="flex items-center gap-3">
              <label className="text-sm w-36">{t(labelKey)}</label>
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
          <h2 className="text-sm font-medium">{t('settings.pricing_data')}</h2>
          {pricing && (
            <span className={[
              'text-xs px-2 py-0.5 rounded-full font-medium',
              pricing.source === 'live' ? 'bg-green-100 text-green-700' :
              pricing.source === 'cache' ? 'bg-yellow-100 text-yellow-700' :
              'bg-gray-100 text-gray-500',
            ].join(' ')}>
              {pricing.source === 'live' ? t('settings.pricing_source_live') :
               pricing.source === 'cache' ? t('settings.pricing_source_cache') :
               pricing.source}
            </span>
          )}
        </div>

        {pricing ? (
          <>
            <div className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
              <span className="text-gray-500">{t('settings.last_synced')}</span>
              <span className="font-mono text-xs">{fmtSyncTime(pricing.last_sync_at)}</span>
              <span className="text-gray-500">{t('settings.models_tracked')}</span>
              <span>{pricing.model_count.toLocaleString()}</span>
              <span className="text-gray-500">{t('settings.cost_this_month')}</span>
              <span>{fmtUSD(pricing.cost_this_month_usd)}</span>
              <span className="text-gray-500">{t('settings.saved_this_month')}</span>
              <span className="text-green-600 font-medium">{fmtUSD(pricing.saved_this_month_usd)}</span>
            </div>

            {pricing.top_models.length > 0 && (
              <div className="space-y-1">
                <p className="text-xs text-gray-400 uppercase tracking-wide">{t('settings.top_models')}</p>
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-100">
                      <th className="text-left py-1 font-normal">{t('settings.col_model')}</th>
                      <th className="text-right py-1 font-normal">{t('settings.col_requests')}</th>
                      <th className="text-right py-1 font-normal">{t('settings.col_cost')}</th>
                      <th className="text-right py-1 font-normal">{t('settings.col_in')}</th>
                      <th className="text-right py-1 font-normal">{t('settings.col_out')}</th>
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
          <p className="text-sm text-gray-400">{t('common.loading')}</p>
        )}
      </section>

      {/* Data Management */}
      <section className="bg-white rounded-xl border border-gray-200 p-5 space-y-4">
        <h2 className="text-sm font-medium">{t('settings.data_management')}</h2>

        {/* Export logs */}
        <div className="space-y-2">
          <p className="text-xs text-gray-500">{t('settings.export_detail')}</p>
          <div className="flex items-center gap-2 flex-wrap">
            <input
              type="date"
              value={exportFrom}
              onChange={(e) => setExportFrom(e.target.value)}
              className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm bg-white"
            />
            <span className="text-sm text-gray-400">{t('settings.date_to')}</span>
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
              {t('settings.export_csv')}
            </a>
          </div>
        </div>

        {/* Reset data */}
        <div className="flex items-center justify-between pt-2 border-t border-gray-50">
          <div>
            <p className="text-sm font-medium text-gray-700">{t('settings.reset_data')}</p>
            <p className="text-xs text-gray-400">{t('settings.reset_detail')}</p>
          </div>
          <button
            onClick={() => {
              if (window.confirm(t('settings.reset_confirm'))) {
                resetData.mutate()
              }
            }}
            disabled={resetData.isPending}
            className="text-sm text-red-600 border border-red-200 rounded-lg px-3 py-1.5 hover:bg-red-50 disabled:opacity-40"
          >
            {resetData.isPending ? t('settings.resetting') : t('settings.reset_data')}
          </button>
        </div>

        {/* Uninstall */}
        <div className="flex items-center justify-between pt-2 border-t border-gray-50">
          <div>
            <p className="text-sm font-medium text-gray-700">{t('settings.uninstall')}</p>
            <p className="text-xs text-gray-400">{t('settings.uninstall_detail')}</p>
          </div>
          <button
            onClick={() => {
              if (window.confirm(t('settings.uninstall_confirm'))) {
                uninstall.mutate()
              }
            }}
            disabled={uninstall.isPending}
            className="text-sm text-red-600 border border-red-200 rounded-lg px-3 py-1.5 hover:bg-red-50 disabled:opacity-40"
          >
            {uninstall.isPending ? t('settings.disconnecting') : t('settings.uninstall')}
          </button>
        </div>
        {(resetData.isError || uninstall.isError) && (
          <p className="text-sm text-red-500">{t('settings.op_failed')}</p>
        )}
      </section>

      {save.isError && (
        <p className="text-sm text-red-500">{t('settings.save_failed')}</p>
      )}
    </div>
  )
}
