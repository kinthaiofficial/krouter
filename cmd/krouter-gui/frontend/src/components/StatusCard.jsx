import React from 'react'

function formatUptime(seconds) {
  if (seconds == null) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

export default function StatusCard({ status, error }) {
  const running = status && !error

  return (
    <div className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">
        Status
      </h2>
      <div className="flex items-center gap-2">
        <span
          className={`inline-block h-2.5 w-2.5 rounded-full ${
            running ? 'bg-green-500' : 'bg-red-400'
          }`}
        />
        <span className="font-medium text-gray-800">
          {running ? 'Running' : 'Offline'}
        </span>
        {running && (
          <span className="ml-auto text-sm text-gray-500">
            Uptime: {formatUptime(status.uptime_seconds)}
          </span>
        )}
      </div>
      {running && (
        <p className="mt-1 text-xs text-gray-400">
          Proxy 127.0.0.1:{status.proxy_port} &nbsp;·&nbsp; v{status.version}
        </p>
      )}
      {error && <p className="mt-1 text-xs text-red-400">{error}</p>}
    </div>
  )
}
