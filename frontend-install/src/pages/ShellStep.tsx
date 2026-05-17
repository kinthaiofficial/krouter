import { useState } from 'react'
import { api } from '../api/client'

interface Props { onNext: () => void }

export default function ShellStep({ onNext }: Props) {
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')
  const [finalizing, setFinalizing] = useState(false)

  async function handleApply() {
    setRunning(true)
    setError('')
    try {
      await api.shellIntegration()
      setDone(true)
    } catch (e) {
      setError((e as Error).message)
      setRunning(false)
    }
  }

  async function handleFinalize() {
    setFinalizing(true)
    try {
      const { redirect_url } = await api.finalize()
      window.location.href = redirect_url
    } catch (e) {
      setError((e as Error).message)
      setFinalizing(false)
    }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">Shell integration</h2>
      <p className="text-sm text-gray-500 mb-6">
        Adds{' '}
        <code className="bg-surface px-1.5 py-0.5 rounded-md text-xs font-mono">eval "$(krouter shell-init)"</code>{' '}
        to your shell RC so <code className="bg-surface px-1.5 py-0.5 rounded-md text-xs font-mono">ANTHROPIC_BASE_URL</code> is set automatically.
      </p>

      {done ? (
        <div className="rounded-xl bg-brand-light border border-brand/20 p-4 text-sm text-gray-700 mb-6 flex items-center gap-2">
          <span className="text-brand font-bold">✓</span>
          Shell integration applied. Restart your terminal to activate.
        </div>
      ) : (
        <div className="rounded-xl bg-surface border border-border p-4 text-sm text-gray-600 mb-6">
          <p>Appends a marker block to <code className="font-mono text-xs">~/.zshrc</code> (or bash / fish equivalent).</p>
          <p className="mt-1 text-gray-400 text-xs">Idempotent — safe to run multiple times.</p>
        </div>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      {!done ? (
        <div className="flex gap-3">
          <button
            onClick={onNext}
            className="flex-1 border border-border text-gray-600 font-medium py-2.5 px-4 rounded-xl hover:bg-surface transition-colors text-sm"
            disabled={running}
          >
            Skip
          </button>
          <button
            onClick={handleApply}
            disabled={running}
            className="flex-1 bg-brand hover:bg-brand-dark disabled:opacity-50 text-white font-semibold py-2.5 px-4 rounded-xl transition-colors"
          >
            {running ? 'Applying…' : 'Apply'}
          </button>
        </div>
      ) : (
        <button
          onClick={handleFinalize}
          disabled={finalizing}
          className="w-full bg-brand hover:bg-brand-dark disabled:opacity-50 text-white font-semibold py-3 px-4 rounded-xl transition-colors"
        >
          {finalizing ? 'Opening dashboard…' : 'Open KRouter Dashboard →'}
        </button>
      )}
    </div>
  )
}
