import { useEffect, useState } from 'react'
import { api, AgentInfo } from '../api/client'

interface Props { onNext: () => void }

const AGENT_LABELS: Record<string, string> = {
  'openclaw':    'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor':      'Cursor',
  'hermes':      'Hermes',
}

export default function DetectStep({ onNext }: Props) {
  const [agents, setAgents] = useState<AgentInfo[] | null>(null)
  const [connecting, setConnecting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.detectAgents().then(setAgents).catch((e: Error) => setError(e.message))
  }, [])

  async function handleConnect() {
    if (!agents) return
    setConnecting(true)
    setError('')
    try {
      for (const a of agents) {
        await api.connectAgent(a.name, a.config_path)
      }
      onNext()
    } catch (e) {
      setError((e as Error).message)
      setConnecting(false)
    }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-gray-900 mb-1 tracking-tight">Detected AI agents</h2>
      <p className="text-sm text-gray-500 mb-6">
        KRouter will patch these agent configs to route through the proxy.
      </p>

      {agents === null && !error && (
        <p className="text-gray-400 text-sm animate-pulse mb-6">Scanning…</p>
      )}

      {agents !== null && agents.length === 0 && (
        <div className="rounded-xl bg-amber-50 border border-amber-100 p-4 text-sm text-amber-700 mb-6">
          No agents found. You can connect them manually later from the KRouter dashboard.
        </div>
      )}

      {agents !== null && agents.length > 0 && (
        <ul className="divide-y divide-border mb-6">
          {agents.map(a => (
            <li key={a.name} className="py-3 flex items-center gap-3">
              <span className="w-5 h-5 rounded-full bg-brand-light flex items-center justify-center text-brand text-xs font-bold">✓</span>
              <div>
                <p className="font-medium text-gray-800">{AGENT_LABELS[a.name] ?? a.name}</p>
                <p className="text-xs text-gray-400">{a.config_path ?? a.cli_path ?? ''}</p>
              </div>
            </li>
          ))}
        </ul>
      )}

      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}

      <div className="flex gap-3">
        <button
          onClick={onNext}
          className="flex-1 border border-border text-gray-600 font-medium py-2.5 px-4 rounded-xl hover:bg-surface transition-colors text-sm"
          disabled={connecting}
        >
          Skip
        </button>
        <button
          onClick={handleConnect}
          disabled={connecting || agents === null}
          className="flex-1 bg-brand hover:bg-brand-dark disabled:opacity-50 text-white font-semibold py-2.5 px-4 rounded-xl transition-colors"
        >
          {connecting ? 'Connecting…' : 'Connect & Continue'}
        </button>
      </div>
    </div>
  )
}
