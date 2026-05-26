import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { CircleDot, ScrollText } from 'lucide-react'
import { api, type LogRecord } from '../api/client'
import { DecisionCard } from '../components/RoutingDecision'
import { PageHeader } from '../components/ui'

const HISTORY_CAP = 200
const INITIAL_VISIBLE = 5
const LOAD_MORE_STEP = 5

export default function Router() {
  const { t } = useTranslation()
  const [sseAlive, setSseAlive] = useState(false)
  const [visibleCount, setVisibleCount] = useState(INITIAL_VISIBLE)
  const sentinelRef = useRef<HTMLDivElement>(null)

  // Seed from recent records.
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
  const visibleHistory = history.slice(0, visibleCount)
  const hasMore = visibleCount < history.length

  // Infinite scroll: load more history when sentinel enters viewport.
  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore) {
          setVisibleCount((v) => v + LOAD_MORE_STEP)
        }
      },
      { rootMargin: '300px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasMore])

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader
        title={t('router.title')}
        subtitle={t('router.subtitle')}
        right={
          <div className="flex items-center gap-2">
            <button
              onClick={() => window.open('/krouter/logs', '_blank')}
              className="inline-flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 hover:text-gray-900 transition-colors font-medium"
            >
              <ScrollText size={13} />
              {t('router.open_logs')}
            </button>
            <LiveBadge alive={sseAlive} t={t} />
          </div>
        }
      />

      {!latest ? (
        <EmptyState t={t} />
      ) : (
        <DecisionCard rec={latest} pulse={pulseId === latest.id} showLatestBadge />
      )}

      {visibleHistory.map((r) => (
        <DecisionCard key={r.id} rec={r} pulse={pulseId === r.id} />
      ))}

      {/* Sentinel — IntersectionObserver attaches here to trigger load-more */}
      <div ref={sentinelRef} className="h-1" />

      {!hasMore && history.length > 0 && (
        <p className="text-center text-xs text-gray-400 pb-4">
          {t('router.all_loaded', { n: feed.length })}
        </p>
      )}
    </div>
  )
}

function LiveBadge({ alive, t }: { alive: boolean; t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full font-medium',
        alive ? 'bg-brand-soft text-brand-ink' : 'bg-gray-100 text-gray-500',
      ].join(' ')}
    >
      <span
        className={[
          'w-1.5 h-1.5 rounded-full',
          alive ? 'bg-brand animate-pulse' : 'bg-gray-400',
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
