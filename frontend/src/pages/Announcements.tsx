import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, CheckCheck, X } from 'lucide-react'

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
    <div className="p-6 space-y-4 max-w-2xl mx-auto">
      <h1 className="text-lg font-semibold">Announcements</h1>

      {isLoading ? (
        <p className="text-sm text-gray-400">Loading…</p>
      ) : items.length === 0 ? (
        <div className="text-center py-16 text-gray-400">
          <p className="text-3xl mb-2">🎉</p>
          <p className="text-sm">No announcements</p>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((ann) => (
            <div
              key={ann.id}
              className={[
                'bg-white dark:bg-gray-800 rounded-xl border p-4 transition-opacity',
                ann.read_at
                  ? 'border-gray-200 dark:border-gray-700 opacity-70'
                  : 'border-blue-200 dark:border-blue-800',
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
                      <span className="text-xs bg-blue-100 dark:bg-blue-900 text-blue-600 dark:text-blue-300 px-1.5 rounded">New</span>
                    )}
                    {ann.priority === 'critical' && (
                      <span className="text-xs bg-red-100 dark:bg-red-900 text-red-600 dark:text-red-300 px-1.5 rounded">Critical</span>
                    )}
                  </div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">
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
                      className="p-1.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                    >
                      <ExternalLink size={14} />
                    </a>
                  )}
                  {!ann.read_at && (
                    <button
                      onClick={() => read.mutate(ann.id)}
                      className="p-1.5 text-gray-400 hover:text-green-600"
                      title="Mark as read"
                    >
                      <CheckCheck size={14} />
                    </button>
                  )}
                  <button
                    onClick={() => dismiss.mutate(ann.id)}
                    className="p-1.5 text-gray-400 hover:text-red-500"
                    title="Dismiss"
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
