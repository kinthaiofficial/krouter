import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

interface Props {
  onNext: () => void
  /** Override for tests: max poll attempts before showing timeout error. */
  maxAttempts?: number
  /** Override for tests: ms between polls. */
  pollIntervalMs?: number
}

export default function ShellStep({ onNext, maxAttempts = 40, pollIntervalMs = 1500 }: Props) {
  const { t } = useTranslation()
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')
  const [launching, setLaunching] = useState(false)

  // Poll daemon-ready while launching, then navigate.
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
        } catch { /* ignore, keep polling */ }
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

  async function handleApply() {
    setRunning(true)
    setError('')
    try {
      await api.shellIntegration()
      setDone(true)
    } catch (e) {
      setError((e as Error).message)
      setRunning(false)
    }
  }

  async function handleOpenDashboard() {
    setError('')
    setLaunching(true)
    try {
      await api.finalize()
    } catch { /* 410 already-finalized is fine */ }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">{t('shell.title')}</h2>
      <p className="text-sm text-gray-500 mb-6">
        {t('shell.detail')}
      </p>

      {done ? (
        <div className="rounded-xl bg-brand-light border border-brand/20 p-4 text-sm text-gray-700 mb-6 flex items-center gap-2">
          <span className="text-brand font-bold">✓</span>
          {t('shell.done')}
        </div>
      ) : (
        <div className="rounded-xl bg-surface border border-border p-4 text-sm text-gray-600 mb-6">
          <p>{t('shell.step_rc')}</p>
          <p className="mt-1 text-gray-500 text-xs">{t('shell.step_idempotent')}</p>
        </div>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      {launching ? (
        <div className="flex flex-col items-center gap-3 py-4">
          <svg className="w-8 h-8 text-brand animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
          </svg>
          <p className="text-sm text-gray-500">{t('done.launching')}</p>
        </div>
      ) : !done ? (
        <div className="flex gap-3">
          <button
            onClick={onNext}
            className="flex-1 border border-border text-gray-600 font-medium py-2.5 px-4 rounded-xl hover:bg-surface transition-colors text-sm"
            disabled={running}
          >
            {t('shell.skip')}
          </button>
          <button
            onClick={handleApply}
            disabled={running}
            className="flex-1 bg-brand hover:bg-brand-dark disabled:opacity-50 text-white font-semibold py-2.5 px-4 rounded-xl transition-colors"
          >
            {running ? t('shell.applying') : t('shell.apply')}
          </button>
        </div>
      ) : (
        <button
          onClick={handleOpenDashboard}
          className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-4 rounded-xl transition-colors"
        >
          {t('done.open')}
        </button>
      )}
    </div>
  )
}
