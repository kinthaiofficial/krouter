import { useEffect, useRef, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AlertCircle, CheckCircle2, ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import { api } from '../api/client'
import { PageHeader } from '../components/ui'

// About page does a fresh upgrade check the moment the user opens it.
// Flow: open page → spinner ("Checking for updates…") → result.
//   - No new version  → green "You're on the latest version"
//   - New version     → yellow banner + "Apply Update" button
//   - Network error   → red "Couldn't check for updates"
//
// Previously the page just read whatever cached state the 24 h ticker
// had last produced, which meant a user who opened the page 10 hours
// after a new release would still see "no update available" until the
// next tick. POST /internal/update-check makes the check synchronous.

interface UpdateStatus {
  current: string
  latest?: string | null
  is_critical?: boolean
  release_notes_url?: string
}

export default function About() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })

  const check = useMutation<UpdateStatus>({
    mutationFn: () =>
      fetch('/internal/update-check', { method: 'POST', credentials: 'include' })
        .then((r) => r.json()),
  })

  const applyUpdate = useMutation({
    mutationFn: () =>
      fetch('/internal/update-apply', { method: 'POST', credentials: 'include' })
        .then((r) => r.json()),
  })

  // applyError holds the error message broadcast by the daemon via SSE when
  // download fails (e.g. network unreachable). Separate from applyUpdate.isError
  // which only reflects the HTTP POST itself.
  const [applyError, setApplyError] = useState<string | null>(null)

  // Fire the fresh check exactly once on mount.
  useEffect(() => {
    check.mutate()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // After apply is triggered, subscribe to SSE for real-time failure/success
  // feedback AND poll /internal/status as a fallback for the restart case.
  const originalVersionRef = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (!applyUpdate.isSuccess) return
    setApplyError(null)
    originalVersionRef.current = status?.version

    // SSE: catches update_apply_failed (network error etc.) and update_restarting.
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.addEventListener('update_apply_failed', (e: MessageEvent) => {
      try {
        const payload = JSON.parse(e.data) as { error?: string }
        setApplyError(payload.error ?? t('about.update_failed'))
      } catch {
        setApplyError(t('about.update_failed'))
      }
      applyUpdate.reset()
      es.close()
    })

    // Polling fallback: detect version change after daemon restart.
    const deadline = Date.now() + 120_000
    const id = setInterval(async () => {
      if (Date.now() > deadline) {
        clearInterval(id)
        applyUpdate.reset()
        return
      }
      try {
        const s: { version: string } = await fetch('/internal/status', { credentials: 'include' }).then((r) => r.json())
        if (s.version !== originalVersionRef.current) {
          clearInterval(id)
          es.close()
          qc.invalidateQueries({ queryKey: ['status'] })
          applyUpdate.reset()
          check.mutate()
        }
      } catch {
        // daemon mid-restart; keep retrying
      }
    }, 1500)

    return () => {
      clearInterval(id)
      es.close()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applyUpdate.isSuccess])

  const data = check.data
  const hasUpdate = !!data?.latest && data.latest !== data.current

  return (
    <div className="p-6 space-y-5 max-w-6xl mx-auto">
      <PageHeader title={t('about.title')} />

      {/* Version + Resources sit side-by-side so the sparse content fills the
          wide (max-w-6xl) container instead of leaving the right half blank. */}
      <div className="grid md:grid-cols-2 gap-5 items-start">
      {/* Version info */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium text-gray-500">{t('about.version')}</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xl font-bold font-mono">{status?.version ?? '…'}</p>
            <p className="text-xs text-gray-400 mt-0.5">krouter</p>
          </div>
          {hasUpdate && (
            <span className="text-xs bg-yellow-100 text-yellow-700 px-2 py-1 rounded-lg">
              {t('about.update_available')}: {data!.latest}
            </span>
          )}
        </div>
        {status && (
          <div className="grid grid-cols-2 gap-2 text-xs text-gray-500 pt-1 border-t border-gray-100">
            <span>{t('about.uptime')}: {formatUptime(status.uptime_seconds)}</span>
            <span>{t('about.pid')}: {status.pid}</span>
            <span>{t('about.proxy_port')}: {status.proxy_port}</span>
            <span>{t('about.api_port')}: {status.mgmt_port}</span>
            {status.build_time && status.build_time !== 'unknown' && (
              <span className="col-span-2">{t('about.built')}: {status.build_time}</span>
            )}
          </div>
        )}
      </div>

      {/* Links */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-2">
        <h2 className="text-sm font-medium text-gray-500">{t('about.resources')}</h2>
        <a
          href="https://kinthai.ai"
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-2 text-sm text-blue-600 hover:underline"
        >
          <ExternalLink size={14} />
          kinthai.ai
        </a>
      </div>
      </div>

      {/* Update check + result — full-width banner below the grid. Always
          rendered so the user gets immediate feedback that the daemon looked. */}
      <UpdateCheckCard
        loading={check.isPending}
        error={check.isError}
        retry={() => { setApplyError(null); check.mutate() }}
        data={data}
        hasUpdate={hasUpdate}
        applyUpdate={applyUpdate}
        applyError={applyError}
        t={t}
      />
    </div>
  )
}

interface UpdateCardProps {
  loading: boolean
  error: boolean
  retry: () => void
  data: UpdateStatus | undefined
  hasUpdate: boolean
  applyUpdate: ReturnType<typeof useMutation<unknown>>
  applyError: string | null
  t: ReturnType<typeof useTranslation>['t']
}

function UpdateCheckCard({ loading, error, retry, data, hasUpdate, applyUpdate, applyError, t }: UpdateCardProps) {
  // 1. Loading — spinner.
  if (loading) {
    return (
      <div className="bg-white rounded-xl border border-gray-200 p-5 flex items-center gap-3 text-sm text-gray-600">
        <Loader2 className="w-4 h-4 animate-spin text-gray-400" />
        {t('about.checking_for_updates')}
      </div>
    )
  }

  // 2. Network / signature error.
  if (error) {
    return (
      <div className="bg-red-50 rounded-xl border border-red-200 p-5 space-y-2 text-sm">
        <p className="text-red-700">{t('about.check_failed')}</p>
        <button
          onClick={retry}
          className="text-xs text-red-700 underline underline-offset-2 hover:text-red-900"
        >
          {t('about.retry_check')}
        </button>
      </div>
    )
  }

  // 3. Up to date.
  if (!hasUpdate) {
    return (
      <div className="bg-white rounded-xl border border-gray-200 p-5 flex items-center gap-3 text-sm text-gray-600">
        <CheckCircle2 className="w-4 h-4 text-emerald-500" />
        {t('about.up_to_date')}
        <button
          onClick={retry}
          className="ml-auto text-xs text-gray-400 hover:text-gray-700 underline underline-offset-2"
        >
          {t('about.recheck')}
        </button>
      </div>
    )
  }

  // 4. Update available — yellow banner with Apply Update button.
  return (
    <div className="bg-yellow-50 rounded-xl border border-yellow-200 p-5 space-y-3">
      <h2 className="text-sm font-medium">{t('about.update_available')}</h2>
      <p className="text-sm text-gray-600">
        {t('about.update_ready', { version: data!.latest })}
        {data!.is_critical && (
          <span className="ml-1 text-red-600 font-medium">{t('about.critical_update')}.</span>
        )}
      </p>
      <div className="flex gap-2">
        <button
          onClick={() => applyUpdate.mutate()}
          disabled={applyUpdate.isPending || applyUpdate.isSuccess}
          className="flex items-center gap-1.5 bg-brand hover:bg-brand-dark text-white text-sm rounded-lg px-4 py-2 disabled:opacity-50"
        >
          <RefreshCw size={14} className={(applyUpdate.isPending || applyUpdate.isSuccess) ? 'animate-spin' : ''} />
          {applyUpdate.isSuccess ? t('about.restarting') : t('about.apply_update')}
        </button>
        {data!.release_notes_url && (
          <a
            href={data!.release_notes_url}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 border border-gray-200 text-sm rounded-lg px-4 py-2 text-gray-600 hover:text-gray-900"
          >
            <ExternalLink size={14} />
            {t('about.release_notes')}
          </a>
        )}
      </div>
      {applyError && (
        <div className="flex items-start gap-2 bg-red-50 border border-red-200 rounded-lg px-3 py-2.5">
          <AlertCircle size={14} className="text-red-500 shrink-0 mt-0.5" />
          <div className="text-xs text-red-700 space-y-0.5">
            <p className="font-medium">{t('about.update_failed')}</p>
            <p className="font-mono break-all opacity-80">{applyError}</p>
            <p className="opacity-70">{t('about.update_failed_hint')}</p>
          </div>
        </div>
      )}
      {!applyError && applyUpdate.isError && (
        <p className="text-xs text-red-500">{t('about.update_failed')}</p>
      )}
    </div>
  )
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}
