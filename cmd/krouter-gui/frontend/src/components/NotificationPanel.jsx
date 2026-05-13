import React, { useEffect } from 'react'

const PRIORITY_COLORS = {
  critical: 'border-l-red-500',
  normal: 'border-l-blue-400',
}

function localise(map, lang) {
  if (!map) return ''
  return map[lang] ?? map['en'] ?? Object.values(map)[0] ?? ''
}

export default function NotificationPanel({ announcements, onClose, onRead, onDismiss }) {
  const lang = navigator.language?.startsWith('zh') ? 'zh-CN' : 'en'

  useEffect(() => {
    function onKey(e) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/20"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Panel */}
      <aside className="relative z-50 flex h-full w-80 flex-col bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3">
          <h2 className="text-sm font-semibold text-gray-800">Notifications</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
            aria-label="Close notifications"
          >
            ✕
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {announcements.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-gray-400">
              No notifications
            </p>
          ) : (
            <ul className="divide-y divide-gray-50">
              {announcements.map((ann) => (
                <li
                  key={ann.id}
                  className={`border-l-4 px-4 py-3 ${
                    PRIORITY_COLORS[ann.priority] ?? PRIORITY_COLORS.normal
                  } ${ann.read_at ? 'bg-white' : 'bg-blue-50'}`}
                  onClick={() => !ann.read_at && onRead(ann.id)}
                  role={!ann.read_at ? 'button' : undefined}
                  style={{ cursor: !ann.read_at ? 'pointer' : 'default' }}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-start gap-2 min-w-0">
                      {ann.icon && (
                        <span className="mt-0.5 flex-shrink-0 text-base">{ann.icon}</span>
                      )}
                      <div className="min-w-0">
                        <p className={`text-sm ${ann.read_at ? 'text-gray-600' : 'font-semibold text-gray-800'}`}>
                          {localise(ann.title, lang)}
                        </p>
                        <p className="mt-0.5 text-xs text-gray-500 line-clamp-2">
                          {localise(ann.summary, lang)}
                        </p>
                        {ann.url && (
                          <a
                            href={ann.url}
                            target="_blank"
                            rel="noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            className="mt-1 text-xs text-blue-500 hover:underline"
                          >
                            Learn more →
                          </a>
                        )}
                      </div>
                    </div>
                    <button
                      onClick={(e) => { e.stopPropagation(); onDismiss(ann.id) }}
                      className="flex-shrink-0 rounded p-0.5 text-gray-300 hover:bg-gray-100 hover:text-gray-500"
                      aria-label="Dismiss"
                    >
                      ✕
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </aside>
    </div>
  )
}
