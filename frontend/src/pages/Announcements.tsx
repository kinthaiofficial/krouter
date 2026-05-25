import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, CheckCheck, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PageHeader } from '../components/ui'

interface Announcement {
  id: string
  type: string
  priority: string
  published_at: string
  expires_at?: string
  title: Record<string, string>
  summary: Record<string, string>
  url: string
  icon?: string
  read_at?: string
  dismissed_at?: string
}

async function fetchAnnouncements(): Promise<Announcement[]> {
  const r = await fetch('/internal/announcements', { credentials: 'include' })
  if (!r.ok) throw new Error('HTTP ' + r.status)
  return r.json()
}

async function markRead(id: string) {
  await fetch('/internal/announcements/read', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  })
}

async function dismissAnn(id: string) {
  await fetch('/internal/announcements/dismiss', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  })
}

export default function Announcements() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: items = [], isLoading } = useQuery({
    queryKey: ['announcements'],
    queryFn: fetchAnnouncements,
    refetchInterval: 60_000,
  })

  const read = useMutation({
    mutationFn: markRead,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['announcements'] })
      qc.invalidateQueries({ queryKey: ['announcements', 'count'] })
    },
  })
  const dismiss = useMutation({
    mutationFn: dismissAnn,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['announcements'] })
      qc.invalidateQueries({ queryKey: ['announcements', 'count'] })
    },
  })

  const lang = navigator.language.startsWith('zh') ? 'zh-CN' : 'en'

  return (
    <div className="p-6 space-y-4 max-w-6xl mx-auto">
      <PageHeader title={t('announcements.title')} />

      {isLoading ? (
        <p className="text-sm text-gray-400">{t('announcements.loading')}</p>
      ) : items.length === 0 ? (
        <div className="text-center py-16 text-gray-400">
          <p className="text-3xl mb-2">🎉</p>
          <p className="text-sm">{t('announcements.none')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((ann) => (
            <div
              key={ann.id}
              className={[
                'bg-card rounded-xl border p-4 transition-opacity',
                ann.read_at
                  ? 'border-line-strong opacity-70'
                  : 'border-blue-200',
              ].join(' ')}
            >
              <div className="flex items-start gap-3">
                {ann.icon && <span className="text-xl shrink-0 mt-0.5">{ann.icon}</span>}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <p className="font-medium text-sm">
                      {ann.title[lang] ?? ann.title.en ?? ''}
                    </p>
                    {!ann.read_at && (
                      <span className="text-xs bg-blue-100 text-blue-600 px-1.5 rounded">{t('announcements.badge_new')}</span>
                    )}
                    {ann.priority === 'critical' && (
                      <span className="text-xs bg-red-100 text-red-600 px-1.5 rounded">{t('announcements.badge_critical')}</span>
                    )}
                  </div>
                  <p className="text-xs text-gray-500">
                    {ann.summary[lang] ?? ann.summary.en ?? ''}
                  </p>
                  <p className="text-xs text-gray-400 mt-1">
                    {new Date(ann.published_at).toLocaleDateString()}
                  </p>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  {ann.url && (
                    <a
                      href={ann.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={() => read.mutate(ann.id)}
                      className="p-1.5 text-gray-400 hover:text-gray-600"
                    >
                      <ExternalLink size={14} />
                    </a>
                  )}
                  {!ann.read_at && (
                    <button
                      onClick={() => read.mutate(ann.id)}
                      className="p-1.5 text-gray-400 hover:text-green-600"
                      title={t('announcements.mark_read')}
                    >
                      <CheckCheck size={14} />
                    </button>
                  )}
                  <button
                    onClick={() => dismiss.mutate(ann.id)}
                    className="p-1.5 text-gray-400 hover:text-red-500"
                    title={t('announcements.dismiss')}
                  >
                    <X size={14} />
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
