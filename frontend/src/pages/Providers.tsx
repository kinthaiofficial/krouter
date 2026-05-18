import { useQuery } from '@tanstack/react-query'
import { CheckCircle, XCircle, AlertCircle, Link, Unlink } from 'lucide-react'

interface ProviderInfo {
  name: string
  protocol: string
  available: boolean
  consecutive_failures: number
  success_rate: number
  last_error_code?: number
}

interface AgentStatus {
  name: string
  config_path?: string
  cli_path?: string
  connected: boolean
  providers?: string[]
}

// Known LLM providers with display labels and setup hints.
const KNOWN_PROVIDERS: Record<string, { label: string; envKey: string }> = {
  anthropic: { label: 'Anthropic', envKey: 'ANTHROPIC_API_KEY' },
  openai: { label: 'OpenAI', envKey: 'OPENAI_API_KEY' },
  deepseek: { label: 'DeepSeek', envKey: 'DEEPSEEK_API_KEY' },
  minimax: { label: 'MiniMax', envKey: 'MINIMAX_API_KEY' },
  moonshot: { label: 'Moonshot', envKey: 'MOONSHOT_API_KEY' },
  qwen: { label: 'Qwen (Alibaba)', envKey: 'DASHSCOPE_API_KEY' },
  groq: { label: 'Groq', envKey: 'GROQ_API_KEY' },
  glm: { label: 'GLM (Zhipu)', envKey: 'ZHIPU_API_KEY' },
}

const AGENT_LABELS: Record<string, string> = {
  'openclaw': 'OpenClaw',
  'claude-code': 'Claude Code',
  'cursor': 'Cursor',
  'hermes': 'Hermes',
}

export default function Providers() {
  const { data: providers = [], isLoading: pvLoading, isError: pvError } = useQuery<ProviderInfo[]>({
    queryKey: ['providers'],
    queryFn: () =>
      fetch('/internal/providers', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<ProviderInfo[]>
      }),
    refetchInterval: 15_000,
  })

  const { data: agents = [], isLoading: agLoading } = useQuery<AgentStatus[]>({
    queryKey: ['agents'],
    queryFn: () =>
      fetch('/internal/agents', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<AgentStatus[]>
      }),
    refetchInterval: 15_000,
  })

  const missingKeys = Object.entries(KNOWN_PROVIDERS).filter(
    ([key]) => !providers.some((p) => p.name === key),
  )

  const isLoading = pvLoading || agLoading

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <h1 className="text-lg font-semibold">Providers</h1>

      {/* ── AI Agents section ── */}
      <div className="space-y-2">
        <h2 className="text-sm font-medium text-gray-500">AI Agents</h2>
        {isLoading ? (
          <p className="text-sm text-gray-400">Loading…</p>
        ) : agents.length === 0 ? (
          <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-4 text-sm text-gray-400">
            No supported AI agents detected (OpenClaw, Claude Code, Cursor, Hermes).
          </div>
        ) : (
          <div className="space-y-2">
            {agents.map((a) => <AgentCard key={a.name} agent={a} />)}
          </div>
        )}
      </div>

      {/* ── LLM Providers section ── */}
      <div className="space-y-2">
        <h2 className="text-sm font-medium text-gray-500">LLM Providers</h2>
        {pvLoading ? (
          <p className="text-sm text-gray-400">Loading…</p>
        ) : pvError ? (
          <p className="text-sm text-red-500">Failed to load providers. Is the daemon running?</p>
        ) : (
          <>
            {providers.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs text-gray-400">Active (API key configured)</p>
                {providers.map((p) => <ProviderCard key={p.name} provider={p} />)}
              </div>
            )}
            {missingKeys.length > 0 && (
              <div className="space-y-2 mt-3">
                <p className="text-xs text-gray-400">Not configured</p>
                {missingKeys.map(([key, meta]) => (
                  <MissingCard key={key} name={key} meta={meta} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function AgentCard({ agent: a }: { agent: AgentStatus }) {
  const label = AGENT_LABELS[a.name] ?? a.name

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-4 flex items-start gap-4">
      <div className="shrink-0 mt-0.5">
        {a.connected ? (
          <Link size={18} className="text-brand" />
        ) : (
          <Unlink size={18} className="text-gray-300" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="font-medium text-sm">{label}</p>
          <span className={[
            'text-xs px-1.5 py-0.5 rounded-full font-medium',
            a.connected
              ? 'bg-brand-light text-brand'
              : 'bg-gray-100 text-gray-400',
          ].join(' ')}>
            {a.connected ? 'Connected' : 'Not connected'}
          </span>
        </div>
        {a.config_path && (
          <p className="text-xs text-gray-400 font-mono truncate mt-0.5">{a.config_path}</p>
        )}
        {a.cli_path && (
          <p className="text-xs text-gray-400 font-mono truncate mt-0.5">{a.cli_path}</p>
        )}
        {a.providers && a.providers.length > 0 && (
          <p className="text-xs text-gray-500 mt-1">
            Configured providers: {a.providers.join(', ')}
          </p>
        )}
      </div>
    </div>
  )
}

function ProviderCard({ provider: p }: { provider: ProviderInfo }) {
  const pct = Math.round(p.success_rate * 100)
  const meta = KNOWN_PROVIDERS[p.name]
  const healthy = p.consecutive_failures === 0 && p.available

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-4 flex items-center gap-4">
      <div className="shrink-0">
        {healthy ? (
          <CheckCircle size={20} className="text-green-500" />
        ) : (
          <XCircle size={20} className="text-red-500" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm">{meta?.label ?? p.name}</p>
        <p className="text-xs text-gray-500 font-mono">{p.protocol}</p>
      </div>
      <div className="text-right shrink-0">
        <p className="text-sm font-mono">{pct}%</p>
        <p className="text-xs text-gray-400">success rate</p>
      </div>
      {p.consecutive_failures > 0 && (
        <div className="text-right shrink-0">
          <p className="text-sm text-red-500">{p.consecutive_failures}</p>
          <p className="text-xs text-gray-400">failures</p>
        </div>
      )}
    </div>
  )
}

function MissingCard({ meta }: { name: string; meta: { label: string; envKey: string } }) {
  return (
    <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-4 flex items-center gap-4">
      <AlertCircle size={20} className="text-gray-300 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm text-gray-400">{meta.label}</p>
        <p className="text-xs text-gray-400 font-mono">Set {meta.envKey} to enable</p>
      </div>
    </div>
  )
}
