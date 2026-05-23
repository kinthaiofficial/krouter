import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Gift, ExternalLink, Check, AlertTriangle, ChevronDown, ChevronUp } from 'lucide-react'
import { api, type FreeProvider } from '../api/client'

// FreeProvidersCard surfaces spec/06's curated catalog of LLM providers
// offering free credits / quotas. The card has two jobs:
//
//   1. Discovery — make sure users *know* DeepSeek / Groq / etc. give
//      out free tokens; one-click signup keeps friction low.
//
//   2. Configured state — when an inherited_endpoints row matches a
//      catalog entry, show a "✓ configured" badge so the user knows
//      routing is already preferring it (no extra setup required;
//      spec/06 §2: routing is fully automatic).
//
// The "Routing automatically prefers configured free providers" line is
// the key user education — without it the discovery list reads like
// "advertisements" rather than "things you've already enabled".
export default function FreeProvidersCard() {
  const { data, isLoading } = useQuery({
    queryKey: ['free-providers'],
    queryFn: api.freeProviders,
    staleTime: 5 * 60_000, // catalog rarely changes; cache 5 min
  })

  const [showConfigured, setShowConfigured] = useState(false)

  const { configured, available } = useMemo(() => {
    const cfg: FreeProvider[] = []
    const avail: FreeProvider[] = []
    for (const p of data ?? []) {
      if (p.user_configured) cfg.push(p)
      else avail.push(p)
    }
    return { configured: cfg, available: avail }
  }, [data])

  if (isLoading) return null
  if (!data || data.length === 0) return null

  return (
    <section
      data-testid="free-providers-card"
      className="bg-white border border-border rounded-2xl p-5 shadow-sm"
    >
      <header className="flex items-baseline justify-between mb-2">
        <div className="flex items-center gap-2">
          <Gift className="w-4 h-4 text-emerald-500" />
          <h2 className="text-sm font-semibold">Free LLM credits</h2>
        </div>
        <span className="text-xs text-gray-400">
          {configured.length} configured · {available.length} to claim
        </span>
      </header>

      <p className="text-[12px] text-gray-500 mb-2 leading-relaxed">
        Apply at the signup link, paste the API key into your AI agent
        (OpenClaw / Claude Code / etc.). krouter detects the new key via
        agent inheritance and{' '}
        <span className="font-medium text-gray-700">automatically prefers free providers</span>{' '}
        until their quota runs out.
      </p>

      <p className="text-[11px] text-gray-400 mb-4 leading-relaxed border-l-2 border-gray-100 pl-2">
        协议约束 (spec/00 §B2): 免费路由必须同协议匹配。表中绝大多数 provider 是
        OpenAI 协议 — Cursor/Cline/Codex 等 OpenAI 协议客户端立即享有免费 routing,
        而 Claude Code 等 Anthropic 协议客户端只有在 provider 同时支持 Anthropic
        endpoint (例如 OpenRouter / GLM / Moonshot) 时才有效。下方双协议 provider 旁
        会显示 "也支持 Anthropic 协议" 提示,需要在 agent 里同时配置 OpenAI 和
        Anthropic 两个 provider entry (同一个 key,不同 baseURL)。
      </p>

      {/* Available (not yet configured) — primary CTA */}
      {available.length > 0 && (
        <ul className="space-y-2 mb-3">
          {available.map((p) => (
            <ProviderRow key={p.id} p={p} />
          ))}
        </ul>
      )}

      {/* Configured — collapsed by default to keep the card compact */}
      {configured.length > 0 && (
        <div className="pt-2 border-t border-gray-100">
          <button
            type="button"
            onClick={() => setShowConfigured((v) => !v)}
            className="flex items-center gap-1 text-xs text-gray-500 hover:text-gray-900"
          >
            {showConfigured ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
            <span>{configured.length} already configured</span>
          </button>
          {showConfigured && (
            <ul className="space-y-2 mt-2">
              {configured.map((p) => (
                <ProviderRow key={p.id} p={p} />
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  )
}

function ProviderRow({ p }: { p: FreeProvider }) {
  return (
    <li className="border border-border rounded-lg p-3">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <p className="font-medium text-sm">{p.display_name}</p>

            <span className={`text-[10px] font-medium rounded-full px-1.5 py-0.5 ${
              p.region === 'china'
                ? 'bg-red-50 text-red-700'
                : 'bg-blue-50 text-blue-700'
            }`}>
              {p.region === 'china' ? '国内' : 'INT\'L'}
            </span>

            <span className="text-[10px] font-medium rounded-full px-1.5 py-0.5 bg-gray-100 text-gray-600">
              {p.free_type === 'trial_credit' ? '试用赠送'
                : p.free_type === 'daily_quota' ? '永久免费'
                : '免费层'}
            </span>

            {p.user_configured && (
              <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-emerald-700 bg-emerald-50 rounded-full px-1.5 py-0.5">
                <Check className="w-3 h-3" />
                {p.source_agent ? `via ${p.source_agent}` : 'configured'}
              </span>
            )}

            {p.exhausted && (
              <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-amber-700 bg-amber-50 rounded-full px-1.5 py-0.5">
                <AlertTriangle className="w-3 h-3" />
                exhausted
              </span>
            )}
          </div>

          <p className="text-[12px] text-gray-600 leading-snug">{p.free_summary}</p>

          <div className="mt-1 text-[11px] text-gray-400 flex gap-3 flex-wrap">
            <span>有效期: {p.validity || 'unknown'}</span>
            <span>申请条件: {p.conditions || 'unknown'}</span>
          </div>

          {p.notes && (
            <p className="text-[11px] text-gray-400 mt-0.5">{p.notes}</p>
          )}
          {p.exhausted_reason && (
            <p className="text-[11px] text-amber-600 mt-0.5">{p.exhausted_reason}</p>
          )}

          {/* Dual-protocol providers: surface the alternate setup so
              Anthropic-protocol clients can also benefit. */}
          {p.additional_protocols && p.additional_protocols.length > 0 && (
            <div className="mt-2 rounded-md border border-indigo-100 bg-indigo-50 p-2">
              <p className="text-[11px] font-medium text-indigo-900 mb-1">
                💡 也支持 {p.additional_protocols.map((a) => a.protocol).join(' / ')} 协议
              </p>
              <ul className="space-y-1.5">
                {p.additional_protocols.map((a) => (
                  <li key={a.krouter_provider_name} className="text-[11px] text-indigo-700">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span className="font-mono bg-white px-1 rounded">{a.protocol}</span>
                      <span>→</span>
                      <span className="font-mono bg-white px-1 rounded">{a.krouter_provider_name}</span>
                      {a.user_configured ? (
                        <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-emerald-700 bg-emerald-50 rounded-full px-1.5 py-0.5">
                          <Check className="w-3 h-3" />
                          {a.source_agent ? `via ${a.source_agent}` : 'configured'}
                        </span>
                      ) : (
                        <span className="text-[10px] text-indigo-500">未配置</span>
                      )}
                    </div>
                    {a.key_setup_hint && (
                      <p className="text-[10px] text-indigo-600 mt-0.5 leading-snug">
                        {a.key_setup_hint}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <p className="text-[10px] text-gray-300 mt-1">
            信息核对于 {p.last_verified || 'unknown'} — 政策可能已更新,请以官网为准
          </p>
        </div>

        <a
          href={p.signup_url}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1 text-xs px-3 py-1.5 rounded-lg bg-brand text-white hover:bg-brand-dark transition-colors whitespace-nowrap"
        >
          {p.user_configured ? '官网' : '去申请'}
          <ExternalLink className="w-3 h-3" />
        </a>
      </div>
    </li>
  )
}
