import React, { useEffect, useState } from 'react'

const AGENT_LABELS = {
  openclaw: 'OpenClaw',
  hermes: 'Hermes',
  cursor: 'Cursor',
  'claude-code': 'Claude Code',
}

export default function Step4Agents({ onNext }) {
  const [agents, setAgents] = useState([])
  const [selected, setSelected] = useState({})
  const [connecting, setConnecting] = useState(false)
  const [errors, setErrors] = useState({})

  useEffect(() => {
    async function detect() {
      const list = window.go?.main?.App?.GetInstalledAgents
        ? await window.go.main.App.GetInstalledAgents()
        : []
      setAgents(list)
      const sel = {}
      list.forEach((a) => { sel[a.name] = true })
      setSelected(sel)
    }
    detect()
  }, [])

  async function handleConnect() {
    setConnecting(true)
    const errs = {}
    for (const agent of agents) {
      if (!selected[agent.name]) continue
      const err = window.go?.main?.App?.ConnectAgent
        ? await window.go.main.App.ConnectAgent(agent.name)
        : ''
      if (err) errs[agent.name] = err
    }
    setErrors(errs)
    setConnecting(false)
    onNext()
  }

  return (
    <div className="space-y-5">
      <h2 className="text-xl font-semibold text-gray-800">Connect your agents</h2>
      {agents.length === 0 ? (
        <p className="text-sm text-gray-500">No agents detected. You can connect them later in Settings.</p>
      ) : (
        <ul className="space-y-2">
          {agents.map((a) => (
            <li key={a.name} className="flex items-center gap-3 rounded-lg border border-gray-100 bg-gray-50 px-4 py-3">
              <input
                type="checkbox"
                checked={!!selected[a.name]}
                onChange={(e) => setSelected((s) => ({ ...s, [a.name]: e.target.checked }))}
                className="h-4 w-4 rounded border-gray-300 text-blue-600"
              />
              <span className="flex-1 text-sm font-medium text-gray-700">
                {AGENT_LABELS[a.name] || a.name}
              </span>
              <span className="text-xs text-gray-400 truncate max-w-[180px]">{a.path}</span>
              {errors[a.name] && (
                <span className="text-xs text-red-500">{errors[a.name]}</span>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="flex justify-between pt-2">
        <button
          onClick={onNext}
          className="text-sm text-gray-400 hover:text-gray-600"
        >
          Skip
        </button>
        <button
          onClick={handleConnect}
          disabled={connecting}
          className="rounded-lg bg-blue-600 px-6 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
        >
          {connecting ? 'Connecting…' : 'Connect'}
        </button>
      </div>
    </div>
  )
}
