import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

interface Props {
  /** Override for tests: max poll attempts before showing timeout error. */
  maxAttempts?: number
  /** Override for tests: ms between polls. */
  pollIntervalMs?: number
}

export default function DoneStep({ maxAttempts = 40, pollIntervalMs = 1500 }: Props) {
  const { t } = useTranslation()
  const [launching, setLaunching] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!launching) return
    let stopped = false
    let attempts = 0

    async function poll() {
      while (!stopped && attempts < maxAttempts) {
        attempts++
        try {
          const res = await api.daemonReady()
          if (res.ready && res.redirect_url) {
            window.location.href = res.redirect_url
            return
          }
        } catch { /* ignore */ }
        await new Promise(r => setTimeout(r, pollIntervalMs))
      }
      if (!stopped) {
        setError(t('done.timeout'))
        setLaunching(false)
      }
    }
    poll()
    return () => { stopped = true }
  }, [launching, maxAttempts, pollIntervalMs])

  async function handleOpen() {
    setError('')
    setLaunching(true)
    try {
      await api.finalize()
    } catch { /* 410 already-finalized is fine */ }
  }

  return (
    <div className="text-center">
      <div className="w-16 h-16 rounded-full bg-brand-light flex items-center justify-center mx-auto mb-5">
        <svg className="w-8 h-8 text-brand" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
        </svg>
      </div>
      <h2 className="text-2xl font-bold text-gray-900 mb-2 tracking-tight">{t('done.title')}</h2>
      <p className="text-gray-500 mb-6 leading-relaxed">
        {t('done.detail')}
      </p>

      {launching ? (
        <div className="flex flex-col items-center gap-3 py-4">
          <svg className="w-8 h-8 text-brand animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
          </svg>
          <p className="text-sm text-gray-500">{t('done.launching')}</p>
        </div>
      ) : (
        <button
          onClick={handleOpen}
          className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-6 rounded-xl transition-colors text-center"
        >
          {t('done.open')}
        </button>
      )}

      {error && (
        <p className="text-red-500 text-sm mt-4">{error}</p>
      )}
      <p className="text-xs text-gray-400 mt-4">
        {t('done.dismiss')}
      </p>
    </div>
  )
}
