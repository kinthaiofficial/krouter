import { useQuery } from '@tanstack/react-query'
import { CheckCircle, XCircle, AlertCircle } from 'lucide-react'

interface ProviderInfo {
  name: string
  protocol: string
  available: boolean
  configured: boolean
  consecutive_failures: number
  success_rate: number
  last_error_code?: number
}

// Known LLM providers with display labels and setup hints.
const KNOWN_PROVIDERS: Record<string, { label: string; envKey: string; settingsKey: string }> = {
  anthropic: { label: 'Anthropic', envKey: 'ANTHROPIC_API_KEY', settingsKey: '' },
  deepseek: { label: 'DeepSeek', envKey: 'DEEPSEEK_API_KEY', settingsKey: 'deepseek' },
  groq: { label: 'Groq', envKey: 'GROQ_API_KEY', settingsKey: 'groq' },
  moonshot: { label: 'Moonshot', envKey: 'MOONSHOT_API_KEY', settingsKey: 'moonshot' },
  qwen: { label: 'Qwen (Alibaba)', envKey: 'DASHSCOPE_API_KEY', settingsKey: 'qwen' },
  glm: { label: 'GLM (Zhipu)', envKey: 'ZHIPU_API_KEY', settingsKey: 'glm' },
  minimax: { label: 'MiniMax', envKey: 'MINIMAX_API_KEY', settingsKey: 'minimax' },
}

export default function Providers() {
  const { data: providers = [], isLoading, isError } = useQuery<ProviderInfo[]>({
    queryKey: ['providers'],
    queryFn: () =>
      fetch('/internal/providers', { credentials: 'include' }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<ProviderInfo[]>
      }),
    refetchInterval: 15_000,
  })

  const activeProviders = providers.filter((p) => p.configured)
  const inactiveProviders = providers.filter((p) => !p.configured)

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <h1 className="text-lg font-semibold">Providers</h1>

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-sm text-gray-400">Loading…</p>
        ) : isError ? (
          <p className="text-sm text-red-500">Failed to load providers. Is the daemon running?</p>
        ) : (
          <>
            {activeProviders.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs text-gray-400">Active (API key configured)</p>
                {activeProviders.map((p) => <ProviderCard key={p.name} provider={p} />)}
              </div>
            )}
            {inactiveProviders.length > 0 && (
              <div className="space-y-2 mt-3">
                <p className="text-xs text-gray-400">Not configured</p>
                {inactiveProviders.map((p) => {
                  const meta = KNOWN_PROVIDERS[p.name]
                  return <MissingCard key={p.name} name={p.name} meta={meta ?? { label: p.name, envKey: '', settingsKey: '' }} />
                })}
              </div>
            )}
          </>
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

function MissingCard({ meta }: { name: string; meta: { label: string; envKey: string; settingsKey: string } }) {
  const hint = meta.envKey
    ? `Set ${meta.envKey} env var, or add "${meta.settingsKey}" to provider_keys in ~/.kinthai/settings.json`
    : 'No API key required'
  return (
    <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-4 flex items-center gap-4">
      <AlertCircle size={20} className="text-gray-300 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm text-gray-400">{meta.label}</p>
        <p className="text-xs text-gray-400">{hint}</p>
      </div>
    </div>
  )
}
