import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCircle, XCircle, AlertCircle, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, type ProviderInfo, type AddProviderBody } from '../api/client'

export default function Providers() {
  const { t } = useTranslation()
  const [showAdd, setShowAdd] = useState(false)
  const { data: providers = [], isLoading, isError } = useQuery<ProviderInfo[]>({
    queryKey: ['providers'],
    queryFn: api.providers,
    refetchInterval: 15_000,
  })

  const active = providers.filter((p) => p.configured)
  const inactive = providers.filter((p) => !p.configured)

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">{t('providers.title')}</h1>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-1.5 text-sm border border-gray-200 rounded-lg px-3 py-1.5 hover:border-blue-400 hover:text-blue-600"
        >
          <Plus size={14} />
          Add Provider
        </button>
      </div>

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-sm text-gray-400">{t('common.loading')}</p>
        ) : isError ? (
          <p className="text-sm text-red-500">Failed to load providers. Is the daemon running?</p>
        ) : (
          <>
            {active.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs text-gray-400 uppercase tracking-wide">{t('providers.active')}</p>
                {active.map((p) => <ActiveCard key={p.name} provider={p} />)}
              </div>
            )}
            {inactive.length > 0 && (
              <div className="space-y-2 mt-4">
                <p className="text-xs text-gray-400 uppercase tracking-wide">{t('providers.not_configured')}</p>
                {inactive.map((p) => <InactiveCard key={p.name} provider={p} />)}
              </div>
            )}
          </>
        )}
      </div>

      {showAdd && <AddProviderDialog onClose={() => setShowAdd(false)} />}
    </div>
  )
}

function ActiveCard({ provider: p }: { provider: ProviderInfo }) {
  const { t } = useTranslation()
  const pct = Math.round(p.success_rate * 100)
  const healthy = p.consecutive_failures === 0 && p.available
  const [testResult, setTestResult] = useState<{ latency_ms: number; status_code: number; ok: boolean } | null>(null)
  const qc = useQueryClient()

  const test = useMutation({
    mutationFn: () => api.testProvider(p.name),
    onSuccess: (res) => setTestResult(res),
  })
  const remove = useMutation({
    mutationFn: () => api.removeProvider(p.name),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
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
        <p className="font-medium text-sm">{p.display_name || p.name}</p>
        <p className="text-xs text-gray-400 font-mono truncate">{p.base_url || p.protocol}</p>
      </div>
      <div className="text-right shrink-0">
        <p className="text-sm font-mono">{pct}%</p>
        <p className="text-xs text-gray-400">success</p>
      </div>
      {(p.requests_today > 0 || p.latency_p50_ms > 0) && (
        <div className="text-right shrink-0">
          <p className="text-sm font-mono">{p.requests_today}</p>
          <p className="text-xs text-gray-400">{t('providers.success_today')}</p>
        </div>
      )}
      {p.latency_p50_ms > 0 && (
        <div className="text-right shrink-0">
          <p className="text-sm font-mono">{p.latency_p50_ms}ms</p>
          <p className="text-xs text-gray-400">{t('providers.p50_lat')}</p>
        </div>
      )}
      {p.consecutive_failures > 0 && (
        <div className="text-right shrink-0">
          <p className="text-sm text-red-500">{p.consecutive_failures}</p>
          <p className="text-xs text-gray-400">{t('providers.failures')}</p>
        </div>
      )}
      <div className="shrink-0 flex flex-col items-end gap-1">
        <button
          onClick={() => test.mutate()}
          disabled={test.isPending}
          className="text-xs border border-gray-200 rounded-lg px-2.5 py-1 hover:border-blue-400 hover:text-blue-600 disabled:opacity-40"
        >
          {test.isPending ? t('providers.testing') : t('providers.test')}
        </button>
        {testResult && (
          <span className={`text-xs font-mono ${testResult.ok ? 'text-green-600' : 'text-red-500'}`}>
            {testResult.latency_ms}ms · {testResult.status_code}
          </span>
        )}
        {!p.is_builtin && (
          <button
            onClick={() => remove.mutate()}
            disabled={remove.isPending}
            className="text-xs text-red-400 hover:text-red-600 disabled:opacity-40"
          >
            <Trash2 size={13} />
          </button>
        )}
      </div>
    </div>
  )
}

function InactiveCard({ provider: p }: { provider: ProviderInfo }) {
  const [expanded, setExpanded] = useState(false)
  const [key, setKey] = useState('')
  const qc = useQueryClient()

  const setKeyMutation = useMutation({
    mutationFn: () => api.setProviderKey(p.name, key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      setExpanded(false)
      setKey('')
    },
  })
  const remove = useMutation({
    mutationFn: () => api.removeProvider(p.name),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['providers'] }),
  })

  const envHint = `${p.name.toUpperCase().replace(/-/g, '_')}_API_KEY`

  return (
    <div className="bg-gray-50 rounded-xl border border-dashed border-gray-200 p-4">
      <div className="flex items-center gap-4">
        <AlertCircle size={20} className="text-gray-300 shrink-0" />
        <div className="flex-1 min-w-0">
          <p className="font-medium text-sm text-gray-500">{p.display_name || p.name}</p>
          <p className="text-xs text-gray-400 font-mono truncate">{p.base_url || `env: ${envHint}`}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {!p.is_builtin && (
            <button
              onClick={() => remove.mutate()}
              disabled={remove.isPending}
              className="text-xs text-red-400 hover:text-red-600 disabled:opacity-40"
            >
              <Trash2 size={13} />
            </button>
          )}
          <button
            onClick={() => setExpanded(!expanded)}
            className="text-xs text-blue-500 hover:text-blue-700 border border-blue-200 rounded-lg px-2.5 py-1 hover:border-blue-400"
          >
            {expanded ? 'Cancel' : 'Set Key'}
          </button>
        </div>
      </div>
      {expanded && (
        <div className="mt-3 flex gap-2">
          <input
            type="password"
            placeholder={`API key (or set ${envHint} env var)`}
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && key && setKeyMutation.mutate()}
            className="flex-1 text-sm border border-gray-200 rounded-lg px-3 py-1.5 focus:outline-none focus:border-blue-400"
          />
          <button
            onClick={() => setKeyMutation.mutate()}
            disabled={!key || setKeyMutation.isPending}
            className="text-sm px-3 py-1.5 bg-blue-600 text-white rounded-lg disabled:opacity-40 hover:bg-blue-700"
          >
            {setKeyMutation.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      )}
    </div>
  )
}

function AddProviderDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [form, setForm] = useState<AddProviderBody>({
    name: '',
    display_name: '',
    base_url: '',
    path_prefix: '',
    protocol: 'openai',
    api_key: '',
  })
  const [error, setError] = useState('')

  const add = useMutation({
    mutationFn: () => api.addProvider(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      onClose()
    },
    onError: (e: Error) => setError(e.message),
  })

  const set = (field: keyof AddProviderBody) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl p-6 w-full max-w-md space-y-4 shadow-xl">
        <div>
          <h2 className="font-semibold text-base">Add Custom Provider</h2>
          <p className="text-xs text-gray-500 mt-0.5">Any OpenAI-compatible LLM API endpoint.</p>
        </div>

        <div className="space-y-3">
          <div>
            <label className="text-xs text-gray-500 block mb-1">Name <span className="text-red-400">*</span></label>
            <input
              value={form.name}
              onChange={set('name')}
              placeholder="e.g. my-llm"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
            <p className="text-xs text-gray-400 mt-0.5">Lowercase letters, digits, - or _</p>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">Display Name</label>
            <input
              value={form.display_name}
              onChange={set('display_name')}
              placeholder="My LLM"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">Base URL <span className="text-red-400">*</span></label>
            <input
              value={form.base_url}
              onChange={set('base_url')}
              placeholder="https://api.example.com"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-gray-500 block mb-1">Path Prefix</label>
              <input
                value={form.path_prefix}
                onChange={set('path_prefix')}
                placeholder="/v1 (default)"
                className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
              />
            </div>
            <div>
              <label className="text-xs text-gray-500 block mb-1">Protocol</label>
              <select
                value={form.protocol}
                onChange={set('protocol')}
                className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400 bg-white"
              >
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
              </select>
            </div>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">API Key</label>
            <input
              type="password"
              value={form.api_key}
              onChange={set('api_key')}
              placeholder="Optional — can add later"
              className="w-full text-sm border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400"
            />
          </div>
        </div>

        {error && <p className="text-xs text-red-500">{error}</p>}

        <div className="flex gap-2 justify-end pt-1">
          <button
            onClick={onClose}
            className="text-sm border border-gray-200 rounded-lg px-4 py-2 hover:border-gray-400"
          >
            Cancel
          </button>
          <button
            onClick={() => add.mutate()}
            disabled={!form.name || !form.base_url || add.isPending}
            className="text-sm bg-blue-600 text-white rounded-lg px-4 py-2 hover:bg-blue-700 disabled:opacity-40"
          >
            {add.isPending ? 'Adding…' : 'Add Provider'}
          </button>
        </div>
      </div>
    </div>
  )
}
