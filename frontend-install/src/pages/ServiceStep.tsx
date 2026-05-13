import { useState } from 'react'
import { api } from '../api/client'

interface Props { onNext: () => void }

export default function ServiceStep({ onNext }: Props) {
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')

  async function handleInstall() {
    setRunning(true)
    setError('')
    try {
      await api.copyBinary()
      await api.registerService()
      setDone(true)
    } catch (e) {
      setError((e as Error).message)
      setRunning(false)
    }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1">Install daemon</h2>
      <p className="text-sm text-gray-500 mb-6">
        krouter will be copied to <code className="bg-gray-100 px-1 rounded">~/.local/bin/krouter</code>{' '}
        and registered as a user service (systemd / LaunchAgent) so it starts automatically.
      </p>

      {done ? (
        <div className="rounded-lg bg-green-50 border border-green-100 p-4 text-sm text-green-700 mb-6">
          ✓ Daemon installed and service registered.
        </div>
      ) : (
        <div className="rounded-lg bg-gray-50 border border-gray-100 p-4 text-sm text-gray-600 mb-6 space-y-1">
          <p>① Copy binary → <code>~/.local/bin/krouter</code></p>
          <p>② Register service (systemd / LaunchAgent)</p>
          <p>③ Enable and start the service</p>
        </div>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      <div className="flex gap-3">
        {!done && (
          <button
            onClick={handleInstall}
            disabled={running}
            className="flex-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium py-2 px-4 rounded-lg transition-colors"
          >
            {running ? 'Installing…' : 'Install'}
          </button>
        )}
        {done && (
          <button
            onClick={onNext}
            className="flex-1 bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-4 rounded-lg transition-colors"
          >
            Continue
          </button>
        )}
      </div>
    </div>
  )
}
