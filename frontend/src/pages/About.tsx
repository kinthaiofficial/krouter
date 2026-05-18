import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import { RefreshCw, ExternalLink } from 'lucide-react'

export default function About() {
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: api.status })
  const { data: updateStatus } = useQuery({
    queryKey: ['update-status'],
    queryFn: () =>
      fetch('/internal/update-status', { credentials: 'include' }).then((r) => r.json()) as
        Promise<{ current: string; latest?: string; is_critical?: boolean; release_notes_url?: string }>,
    refetchInterval: 300_000,
  })

  const applyUpdate = useMutation({
    mutationFn: () =>
      fetch('/internal/update-apply', { method: 'POST', credentials: 'include' }).then((r) => r.json()),
  })

  const hasUpdate = !!updateStatus?.latest && updateStatus.latest !== updateStatus?.current

  return (
    <div className="p-6 space-y-5 max-w-lg mx-auto">
      <h1 className="text-lg font-semibold">About</h1>

      {/* Version info */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-3">
        <h2 className="text-sm font-medium text-gray-500">Version</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xl font-bold font-mono">{status?.version ?? '…'}</p>
            <p className="text-xs text-gray-400 mt-0.5">krouter</p>
          </div>
          {hasUpdate && (
            <span className="text-xs bg-yellow-100 text-yellow-700 px-2 py-1 rounded-lg">
              Update available: {updateStatus!.latest}
            </span>
          )}
        </div>
        {status && (
          <div className="grid grid-cols-2 gap-2 text-xs text-gray-500 pt-1 border-t border-gray-100">
            <span>Uptime: {formatUptime(status.uptime_seconds)}</span>
            <span>PID: {status.pid}</span>
            <span>Proxy port: {status.proxy_port}</span>
            <span>API port: {status.mgmt_port}</span>
          </div>
        )}
      </div>

      {/* Update section */}
      {hasUpdate && (
        <div className="bg-yellow-50 rounded-xl border border-yellow-200 p-5 space-y-3">
          <h2 className="text-sm font-medium">New version available</h2>
          <p className="text-sm text-gray-600">
            Version <strong>{updateStatus!.latest}</strong> is ready to install.
            {updateStatus!.is_critical && (
              <span className="ml-1 text-red-600 font-medium">Critical update.</span>
            )}
          </p>
          <div className="flex gap-2">
            <button
              onClick={() => applyUpdate.mutate()}
              disabled={applyUpdate.isPending || applyUpdate.isSuccess}
              className="flex items-center gap-1.5 bg-brand hover:bg-brand-dark text-white text-sm rounded-lg px-4 py-2 disabled:opacity-50"
            >
              <RefreshCw size={14} className={applyUpdate.isPending ? 'animate-spin' : ''} />
              {applyUpdate.isSuccess ? 'Restarting…' : 'Apply Update'}
            </button>
            {updateStatus!.release_notes_url && (
              <a
                href={updateStatus!.release_notes_url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1.5 border border-gray-200 text-sm rounded-lg px-4 py-2 text-gray-600 hover:text-gray-900"
              >
                <ExternalLink size={14} />
                Release Notes
              </a>
            )}
          </div>
          {applyUpdate.isError && (
            <p className="text-xs text-red-500">Failed to apply update. Please try again.</p>
          )}
        </div>
      )}

      {/* Links */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 space-y-2">
        <h2 className="text-sm font-medium text-gray-500">Resources</h2>
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
  )
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}
