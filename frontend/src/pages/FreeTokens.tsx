import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Gift, ExternalLink, Check, AlertTriangle, ChevronDown, ChevronUp, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, type FreeProvider } from '../api/client'

// FreeTokens is the top-level dashboard page for spec/06's free-credit
// provider catalogue. Previously the same data lived as a card on the
// Dashboard (FreeProvidersCard); promoting it to its own page gives the
// catalogue room to grow (per-provider expand-for-details, regional
// filters, search later) and matches the user's stated mental model —
// "free tokens" is now a peer-level concept next to Router / Agents /
// Providers, not an afterthought on the Dashboard.
export default function FreeTokens() {
  const { t } = useTranslation()
  const { data, isLoading, isError } = useQuery({
    queryKey: ['free-providers'],
    queryFn: api.freeProviders,
    staleTime: 5 * 60_000,
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

  return (
    <div className="p-6 space-y-4 max-w-4xl mx-auto" data-testid="free-tokens-page">
      <div className="flex items-end justify-between flex-wrap gap-2">
        <div>
          <h1 className="text-lg font-bold tracking-tight flex items-center gap-2">
            <Gift className="w-4 h-4 text-brand" />
            {t('freeTokens.title')}
          </h1>
          <p className="text-xs text-gray-500 mt-0.5">{t('freeTokens.subtitle')}</p>
        </div>
        {data && data.length > 0 && (
          <span className="text-xs text-gray-400">
            {t('freeTokens.summary', {
              configured: configured.length,
              available: available.length,
            })}
          </span>
        )}
      </div>

      {isLoading ? (
        <p className="text-sm text-gray-400">{t('common.loading')}</p>
      ) : isError ? (
        <p className="text-sm text-red-500">{t('freeTokens.load_failed')}</p>
      ) : !data || data.length === 0 ? (
        <EmptyState t={t} />
      ) : (
        <div className="space-y-4">
          {/* Help banner — the routing-rules invariant + how to claim. */}
          <section className="bg-white rounded-xl border border-gray-200 p-4 space-y-2">
            <p className="text-[12px] text-gray-600 leading-relaxed">
              {t('freeTokens.howto_line1')}
            </p>
            <div className="flex gap-2 border-l-2 border-gray-100 pl-2">
              <Info className="w-3.5 h-3.5 text-indigo-400 mt-0.5 shrink-0" />
              <p className="text-[11px] text-gray-500 leading-relaxed">
                {t('freeTokens.howto_line2')}
              </p>
            </div>
          </section>

          {/* Available (not yet configured) — primary CTA. */}
          {available.length > 0 && (
            <section className="bg-white rounded-xl border border-gray-200 p-4 space-y-3">
              <h2 className="text-[11px] uppercase tracking-wider text-gray-500 font-semibold">
                {t('freeTokens.section_available', { n: available.length })}
              </h2>
              <ul className="space-y-2">
                {available.map((p) => (
                  <ProviderRow key={p.id} p={p} />
                ))}
              </ul>
            </section>
          )}

          {/* Configured — collapsed by default. */}
          {configured.length > 0 && (
            <section className="bg-white rounded-xl border border-gray-200">
              <button
                type="button"
                onClick={() => setShowConfigured((v) => !v)}
                className="w-full flex items-center gap-2 px-4 py-3 text-sm text-gray-600 hover:bg-gray-50 rounded-xl"
              >
                {showConfigured ? (
                  <ChevronUp className="w-3.5 h-3.5" />
                ) : (
                  <ChevronDown className="w-3.5 h-3.5" />
                )}
                {t('freeTokens.section_configured', { n: configured.length })}
              </button>
              {showConfigured && (
                <ul className="space-y-2 px-4 pb-4">
                  {configured.map((p) => (
                    <ProviderRow key={p.id} p={p} />
                  ))}
                </ul>
              )}
            </section>
          )}
        </div>
      )}
    </div>
  )
}

function EmptyState({ t }: { t: ReturnType<typeof useTranslation>['t'] }) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 px-6 py-12 text-center">
      <Gift className="w-7 h-7 mx-auto text-gray-300 mb-3" />
      <p className="text-sm font-medium text-gray-700">{t('freeTokens.empty_title')}</p>
      <p className="text-xs text-gray-400 mt-1">{t('freeTokens.empty_hint')}</p>
    </div>
  )
}

function ProviderRow({ p }: { p: FreeProvider }) {
  const { t } = useTranslation()

  const freeTypeLabel =
    p.free_type === 'trial_credit' ? t('freeTokens.free_type_trial')
    : p.free_type === 'daily_quota' ? t('freeTokens.free_type_daily')
    : t('freeTokens.free_type_tier')

  return (
    <li className="border border-border rounded-lg p-3">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5 flex-wrap">
            <p className="font-medium text-sm">{p.display_name}</p>

            <span className={`text-[10px] font-medium rounded-full px-1.5 py-0.5 ${
              p.region === 'china'
                ? 'bg-red-50 text-red-700'
                : 'bg-blue-50 text-blue-700'
            }`}>
              {p.region === 'china' ? t('freeTokens.region_china') : t('freeTokens.region_intl')}
            </span>

            <span className="text-[10px] font-medium rounded-full px-1.5 py-0.5 bg-gray-100 text-gray-600">
              {freeTypeLabel}
            </span>

            {p.user_configured && (
              <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-emerald-700 bg-emerald-50 rounded-full px-1.5 py-0.5">
                <Check className="w-3 h-3" />
                {p.source_agent
                  ? t('freeTokens.configured_via', { agent: p.source_agent })
                  : t('freeTokens.configured_badge')}
              </span>
            )}

            {p.exhausted && (
              <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-amber-700 bg-amber-50 rounded-full px-1.5 py-0.5">
                <AlertTriangle className="w-3 h-3" />
                {t('freeTokens.exhausted_badge')}
              </span>
            )}
          </div>

          <p className="text-[12px] text-gray-600 leading-snug">{p.free_summary}</p>

          <div className="mt-1 text-[11px] text-gray-400 flex gap-3 flex-wrap">
            <span>{t('freeTokens.validity_label')} {p.validity || '—'}</span>
            <span>{t('freeTokens.conditions_label')} {p.conditions || '—'}</span>
          </div>

          {p.notes && (
            <p className="text-[11px] text-gray-400 mt-0.5">{p.notes}</p>
          )}
          {p.exhausted_reason && (
            <p className="text-[11px] text-amber-600 mt-0.5">{p.exhausted_reason}</p>
          )}

          {/* Dual-protocol providers — surface alternate setup so
              Anthropic-protocol clients can also benefit. */}
          {p.additional_protocols && p.additional_protocols.length > 0 && (
            <div className="mt-2 rounded-md border border-indigo-100 bg-indigo-50 p-2">
              <p className="text-[11px] font-medium text-indigo-900 mb-1 flex items-center gap-1">
                <Info className="w-3 h-3 text-indigo-500" />
                {t('freeTokens.dual_protocol_hint', {
                  protocols: p.additional_protocols.map((a) => a.protocol).join(' / '),
                })}
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
                          {a.source_agent
                            ? t('freeTokens.configured_via', { agent: a.source_agent })
                            : t('freeTokens.configured_badge')}
                        </span>
                      ) : (
                        <span className="text-[10px] text-indigo-500">{t('freeTokens.not_configured')}</span>
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

          {p.last_verified && (
            <p className="text-[10px] text-gray-300 mt-1">
              {t('freeTokens.verified_at', { date: p.last_verified })}
            </p>
          )}
        </div>

        <a
          href={p.signup_url}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1 text-xs px-3 py-1.5 rounded-lg bg-brand text-white hover:bg-brand-dark transition-colors whitespace-nowrap"
        >
          {p.user_configured ? t('freeTokens.visit_site') : t('freeTokens.apply')}
          <ExternalLink className="w-3 h-3" />
        </a>
      </div>
    </li>
  )
}
