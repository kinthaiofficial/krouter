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
      <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">Install daemon</h2>
      <p className="text-sm text-gray-500 mb-6">
        KRouter will be copied to{' '}
        <code className="bg-surface px-1.5 py-0.5 rounded-md text-xs font-mono">~/.local/bin/krouter</code>{' '}
        and registered as a user service so it starts automatically.
      </p>

      {done ? (
        <div className="rounded-xl bg-brand-light border border-brand/20 p-4 text-sm text-gray-700 mb-6 flex items-center gap-2">
          <span className="text-brand font-bold">✓</span>
          Daemon installed and service registered.
        </div>
      ) : (
        <div className="rounded-xl bg-surface border border-border p-4 text-sm text-gray-600 mb-6 space-y-2">
          <p className="flex items-center gap-2"><span className="text-gray-400">①</span> Copy binary → <code className="font-mono text-xs">~/.local/bin/krouter</code></p>
          <p className="flex items-center gap-2"><span className="text-gray-400">②</span> Register service (systemd / LaunchAgent)</p>
          <p className="flex items-center gap-2"><span className="text-gray-400">③</span> Enable and start the service</p>
        </div>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      {!done ? (
        <button
          onClick={handleInstall}
          disabled={running}
          className="w-full bg-brand hover:bg-brand-dark disabled:opacity-50 text-white font-semibold py-3 px-4 rounded-xl transition-colors"
        >
          {running ? 'Installing…' : 'Install'}
        </button>
      ) : (
        <button
          onClick={onNext}
          className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-4 rounded-xl transition-colors"
        >
          Continue →
        </button>
      )}
    </div>
  )
}
