import { useState } from 'react'
import { api } from '../api/client'

interface Props { onNext: () => void }

export default function ShellStep({ onNext }: Props) {
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')

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
    try {
      const { redirect_url } = await api.finalize()
      window.location.href = redirect_url
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1">Shell integration</h2>
      <p className="text-sm text-gray-500 mb-6">
        Adds <code className="bg-gray-100 px-1 rounded">eval "$(krouter shell-init)"</code> to
        your shell RC file so{' '}
        <code className="bg-gray-100 px-1 rounded">ANTHROPIC_BASE_URL</code> is set automatically.
      </p>

      {done ? (
        <div className="rounded-lg bg-green-50 border border-green-100 p-4 text-sm text-green-700 mb-6">
          ✓ Shell integration applied. Restart your terminal to activate.
        </div>
      ) : (
        <div className="rounded-lg bg-gray-50 border border-gray-100 p-4 text-sm text-gray-600 mb-6">
          <p>Appends a marker block to <code>~/.zshrc</code> (or bash/fish equivalent).</p>
          <p className="mt-1 text-gray-400">Idempotent — safe to run multiple times.</p>
        </div>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      {!done && (
        <div className="flex gap-3">
          <button
            onClick={onNext}
            className="flex-1 border border-gray-200 text-gray-600 font-medium py-2 px-4 rounded-lg hover:bg-gray-50 transition-colors text-sm"
            disabled={running}
          >
            Skip
          </button>
          <button
            onClick={handleApply}
            disabled={running}
            className="flex-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium py-2 px-4 rounded-lg transition-colors"
          >
            {running ? 'Applying…' : 'Apply'}
          </button>
        </div>
      )}

      {done && (
        <button
          onClick={handleFinalize}
          className="w-full bg-green-600 hover:bg-green-700 text-white font-medium py-2 px-4 rounded-lg transition-colors"
        >
          Open krouter Dashboard →
        </button>
      )}
    </div>
  )
}
