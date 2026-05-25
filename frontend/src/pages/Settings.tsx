import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Settings as ISettings } from '../api/client'
import i18n, { storeLang, settingsLangToI18n } from '../i18n'
import { PageHeader, Panel } from '../components/ui'

export default function Settings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
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

  // Sync settings.language to i18n on first load.
  useEffect(() => {
    if (settings?.language) {
      const lang = settingsLangToI18n(settings.language)
      storeLang(lang)
      i18n.changeLanguage(lang)
    }
  }, [settings?.language])

  if (isLoading || !settings) return <div className="p-6 text-sm text-gray-400">{t('common.loading')}</div>

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader title={t('settings.title')} />

      <Panel title={t('settings.language')}>
        <select
          value={settings.language}
          onChange={(e) => {
            const newLang = e.target.value
            const i18nLang = settingsLangToI18n(newLang)
            storeLang(i18nLang)
            i18n.changeLanguage(i18nLang)
            save.mutate({ language: newLang })
          }}
          className="border border-line-strong rounded-lg px-3 py-1.5 text-sm bg-card"
        >
          <option value="en">{t('settings.lang_en')}</option>
          <option value="zh-CN">{t('settings.lang_zh')}</option>
        </select>
      </Panel>

      <Panel title={t('settings.data_management')}>
        <div className="space-y-4">
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
        <div className="flex items-center justify-between pt-3 border-t border-line">
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
        <div className="flex items-center justify-between pt-3 border-t border-line">
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
        </div>
      </Panel>

      {save.isError && (
        <p className="text-sm text-red-500">{t('settings.save_failed')}</p>
      )}
    </div>
  )
}
