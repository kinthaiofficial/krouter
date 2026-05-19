import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { CheckCircle, XCircle, AlertCircle } from 'lucide-react'
import { api, type ProviderInfo } from '../api/client'

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
    queryFn: api.providers,
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
  const [testResult, setTestResult] = useState<{ latency_ms: number; status_code: number; ok: boolean } | null>(null)

  const test = useMutation({
    mutationFn: () => api.testProvider(p.name),
    onSuccess: (res) => setTestResult(res),
  })

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
      {(p.requests_today > 0 || p.latency_p50_ms > 0) && (
        <div className="text-right shrink-0">
          <p className="text-sm font-mono">{p.requests_today}</p>
          <p className="text-xs text-gray-400">today</p>
        </div>
      )}
      {p.latency_p50_ms > 0 && (
        <div className="text-right shrink-0">
          <p className="text-sm font-mono">{p.latency_p50_ms}ms</p>
          <p className="text-xs text-gray-400">p50 lat</p>
        </div>
      )}
      {p.consecutive_failures > 0 && (
        <div className="text-right shrink-0">
          <p className="text-sm text-red-500">{p.consecutive_failures}</p>
          <p className="text-xs text-gray-400">failures</p>
        </div>
      )}
      <div className="shrink-0 flex flex-col items-end gap-1">
        <button
          onClick={() => test.mutate()}
          disabled={test.isPending}
          className="text-xs border border-gray-200 rounded-lg px-2.5 py-1 hover:border-blue-400 hover:text-blue-600 disabled:opacity-40"
        >
          {test.isPending ? 'Testing…' : 'Test'}
        </button>
        {testResult && (
          <span className={`text-xs font-mono ${testResult.ok ? 'text-green-600' : 'text-red-500'}`}>
            {testResult.latency_ms}ms · {testResult.status_code}
          </span>
        )}
      </div>
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
