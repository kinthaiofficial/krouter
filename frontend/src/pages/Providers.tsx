import { useQuery } from '@tanstack/react-query'
import { CheckCircle, XCircle, AlertCircle } from 'lucide-react'

interface ProviderInfo {
  name: string
  protocol: string
  available: boolean
  consecutive_failures: number
  success_rate: number
  last_error_code?: number
}

// Known providers with setup hints.
const KNOWN_PROVIDERS: Record<string, { label: string; envKey: string; docs?: string }> = {
  anthropic: { label: 'Anthropic', envKey: 'ANTHROPIC_API_KEY' },
  openai: { label: 'OpenAI', envKey: 'OPENAI_API_KEY' },
  deepseek: { label: 'DeepSeek', envKey: 'DEEPSEEK_API_KEY' },
  moonshot: { label: 'Moonshot', envKey: 'MOONSHOT_API_KEY' },
  qwen: { label: 'Qwen (Alibaba)', envKey: 'QWEN_API_KEY' },
  groq: { label: 'Groq', envKey: 'GROQ_API_KEY' },
  glm: { label: 'GLM (Zhipu)', envKey: 'GLM_API_KEY' },
}

export default function Providers() {
  const { data: providers = [], isLoading } = useQuery<ProviderInfo[]>({
    queryKey: ['providers'],
    queryFn: () =>
      fetch('/internal/providers', { credentials: 'include' }).then((r) => r.json()),
    refetchInterval: 15_000,
  })

  const configured = providers
  const missingKeys = Object.entries(KNOWN_PROVIDERS).filter(
    ([key]) => !providers.some((p) => p.name === key),
  )

  return (
    <div className="p-6 space-y-5 max-w-3xl mx-auto">
      <h1 className="text-lg font-semibold">Providers</h1>

      {isLoading ? (
        <p className="text-sm text-gray-400">Loading…</p>
      ) : (
        <>
          {configured.length > 0 && (
            <div className="space-y-2">
              <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400">Configured</h2>
              <div className="space-y-2">
                {configured.map((p) => <ProviderCard key={p.name} provider={p} />)}
              </div>
            </div>
          )}

          {missingKeys.length > 0 && (
            <div className="space-y-2">
              <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400">Not configured</h2>
              <div className="space-y-2">
                {missingKeys.map(([key, meta]) => (
                  <MissingCard key={key} name={key} meta={meta} />
                ))}
              </div>
            </div>
          )}

          {configured.length === 0 && missingKeys.length === 0 && (
            <p className="text-sm text-gray-400">No provider data available.</p>
          )}
        </>
      )}
    </div>
  )
}

function ProviderCard({ provider: p }: { provider: ProviderInfo }) {
  const pct = Math.round(p.success_rate * 100)
  const meta = KNOWN_PROVIDERS[p.name]
  const healthy = p.consecutive_failures === 0 && p.available
  const degraded = !p.available || p.consecutive_failures > 0

  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-4 flex items-center gap-4">
      <div className="shrink-0">
        {healthy ? (
          <CheckCircle size={20} className="text-green-500" />
        ) : degraded ? (
          <XCircle size={20} className="text-red-500" />
        ) : (
          <AlertCircle size={20} className="text-yellow-500" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm">{meta?.label ?? p.name}</p>
        <p className="text-xs text-gray-500 dark:text-gray-400 font-mono">{p.protocol}</p>
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
    <div className="bg-gray-50 dark:bg-gray-800/50 rounded-xl border border-dashed border-gray-200 dark:border-gray-600 p-4 flex items-center gap-4">
      <AlertCircle size={20} className="text-gray-300 dark:text-gray-600 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="font-medium text-sm text-gray-400 dark:text-gray-500">{meta.label}</p>
        <p className="text-xs text-gray-400 dark:text-gray-500 font-mono">Set {meta.envKey} to enable</p>
      </div>
    </div>
  )
}
