import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, CircleDot } from 'lucide-react'
import { api, type LogRecord } from '../api/client'
import { DecisionCard, DecisionRow } from '../components/RoutingDecision'

const HISTORY_CAP = 50

export default function Router() {
  const { t } = useTranslation()
  const [sseAlive, setSseAlive] = useState(false)
  const [showHistory, setShowHistory] = useState(false)

  // Seed from the last 50 records — same source the Logs page reads from.
  // NOTE: do NOT default `data` to []; the new array reference each render
  // would re-fire the seed-merge useEffect and create a setState loop.
  const { data: seed } = useQuery({
    queryKey: ['router', 'seed'],
    queryFn: () => api.logs(HISTORY_CAP),
    staleTime: Infinity,
  })

  const [feed, setFeed] = useState<LogRecord[]>([])
  useEffect(() => {
    if (seed) setFeed(seed)
  }, [seed])

  // Track id of the most-recent record so we can pulse-highlight when a new one arrives.
  const latestIdRef = useRef<string | null>(null)
  const [pulseId, setPulseId] = useState<string | null>(null)

  useEffect(() => {
    const es = new EventSource('/internal/events', { withCredentials: true })
    es.onopen = () => setSseAlive(true)
    es.onerror = () => setSseAlive(false)
    es.addEventListener('request_completed', (e) => {
      try {
        const rec = JSON.parse(e.data) as LogRecord
        setFeed((prev) => {
          if (prev.some((r) => r.id === rec.id)) return prev
          return [rec, ...prev].slice(0, HISTORY_CAP)
        })
        latestIdRef.current = rec.id
        setPulseId(rec.id)
        window.setTimeout(() => {
          setPulseId((cur) => (cur === rec.id ? null : cur))
        }, 1500)
      } catch { /* ignore malformed events */ }
    })
    return () => es.close()
  }, [])

  const latest = feed[0]
  const history = feed.slice(1)

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <div className="flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold">{t('router.title')}</h1>
          <p className="text-xs text-gray-400 mt-0.5">{t('router.subtitle')}</p>
        </div>
        <LiveBadge alive={sseAlive} t={t} />
      </div>

      {!latest ? (
        <EmptyState t={t} />
      ) : (
        <DecisionCard rec={latest} pulse={pulseId === latest.id} showLatestBadge />
      )}

      {history.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200">
          <button
            onClick={() => setShowHistory((v) => !v)}
            className="w-full flex items-center justify-between px-4 py-3 text-sm text-gray-600 hover:bg-gray-50 rounded-xl"
          >
            <span className="flex items-center gap-2">
              {showHistory ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              {showHistory
                ? t('router.hide_history')
                : t('router.show_n_more', { n: history.length })}
            </span>
          </button>
          {showHistory && (
            <ul className="divide-y divide-gray-50 border-t border-gray-100">
              {history.map((r) => (
                <DecisionRow key={r.id} r={r} />
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

function LiveBadge({ alive, t }: { alive: boolean; t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full font-medium',
        alive ? 'bg-green-50 text-green-700' : 'bg-gray-100 text-gray-500',
      ].join(' ')}
    >
      <span
        className={[
          'w-1.5 h-1.5 rounded-full',
          alive ? 'bg-green-500 animate-pulse' : 'bg-gray-400',
        ].join(' ')}
      />
      {alive ? t('router.live') : t('router.offline')}
    </span>
  )
}

function EmptyState({ t }: { t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 px-6 py-12 text-center">
      <CircleDot size={28} className="mx-auto text-gray-300 mb-3" />
      <p className="text-sm font-medium text-gray-700">{t('router.waiting')}</p>
      <p className="text-xs text-gray-400 mt-1">{t('router.waiting_hint')}</p>
    </div>
  )
}
