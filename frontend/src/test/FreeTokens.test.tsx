import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import i18n from '../i18n'
import FreeTokens from '../pages/FreeTokens'

type Handler = () => unknown
const handlers = new Map<string, Handler>()

beforeEach(() => {
  handlers.clear()
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    const path = url.split('?')[0]
    const body = handlers.get(path)?.() ?? []
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
})

describe('<FreeTokens>', () => {
  it('shows the empty state when no providers come back', async () => {
    handlers.set('/internal/free-providers', () => [])
    renderWithProviders(<FreeTokens />)
    await waitFor(() => {
      expect(screen.getByText(/No free providers loaded yet|暂无免费 provider 数据/i)).toBeInTheDocument()
    })
  })

  it('lists available providers under the Available section with a signup CTA', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'deepseek',
        display_name: 'DeepSeek',
        krouter_provider_name: 'deepseek',
        protocol: 'openai',
        region: 'china',
        free_type: 'trial_credit',
        free_summary: '新用户赠送 ¥10',
        free_quota_usd: 1.4,
        validity: '30 days',
        conditions: '手机号',
        signup_url: 'https://platform.deepseek.com/sign_up',
        key_setup_hint: 'OpenClaw',
        last_verified: '2026-05-23',
        user_configured: false,
      },
    ])

    renderWithProviders(<FreeTokens />)
    await waitFor(() => screen.getByText('DeepSeek'))

    expect(screen.getByText(/新用户赠送/)).toBeInTheDocument()
    expect(screen.getByText(/Available — 1 to claim|可申领 — 1/)).toBeInTheDocument()

    const link = screen.getByRole('link', { name: /Apply now|去申请/i })
    expect(link).toHaveAttribute('href', 'https://platform.deepseek.com/sign_up')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('collapses configured providers under a click-to-expand section', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'deepseek',
        display_name: 'DeepSeek',
        krouter_provider_name: 'deepseek',
        protocol: 'openai',
        region: 'china',
        free_type: 'trial_credit',
        free_summary: '¥10',
        free_quota_usd: 1.4,
        validity: '30 days',
        conditions: 'phone',
        signup_url: 'https://example.com/ds',
        key_setup_hint: '',
        last_verified: '2026-05-23',
        user_configured: true,
        source_agent: 'openclaw',
      },
    ])

    renderWithProviders(<FreeTokens />)
    await waitFor(() => screen.getByText(/already configured|已配置/))

    // Configured list is collapsed initially → row not in DOM yet.
    expect(screen.queryByText('DeepSeek')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText(/already configured|已配置/))
    await waitFor(() => screen.getByText('DeepSeek'))
  })

  it('renders the page header + subtitle even when waiting on data', async () => {
    handlers.set('/internal/free-providers', () => [])
    renderWithProviders(<FreeTokens />)
    // Header always visible (h1 + subtitle).
    expect(screen.getByRole('heading', { level: 1, name: /Free LLM credits|免费 LLM 额度/ })).toBeInTheDocument()
    expect(screen.getByText(/Curated catalogue|整理好的免费额度/)).toBeInTheDocument()
  })

  it('shows English copy by default and the zh override after switching language', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'zhipu-glm',
        display_name: 'Zhipu GLM',
        krouter_provider_name: 'zai',
        protocol: 'openai',
        region: 'china',
        free_type: 'trial_credit',
        free_summary: 'Sign-up grants 25M tokens',
        free_quota_usd: 3.5,
        validity: '30 days',
        conditions: 'Phone + real-name',
        signup_url: 'https://open.bigmodel.cn',
        key_setup_hint: 'zai key',
        last_verified: '2026-05-23',
        user_configured: false,
        i18n: { zh: { free_summary: '注册赠送 2500万 tokens' } },
      },
    ])

    try {
      renderWithProviders(<FreeTokens />)
      // Default language is English → base field, not the zh override.
      await waitFor(() => screen.getByText('Sign-up grants 25M tokens'))
      expect(screen.queryByText('注册赠送 2500万 tokens')).not.toBeInTheDocument()

      // Switching the UI to Chinese overlays the zh override.
      await i18n.changeLanguage('zh')
      await waitFor(() => screen.getByText('注册赠送 2500万 tokens'))
      expect(screen.queryByText('Sign-up grants 25M tokens')).not.toBeInTheDocument()
    } finally {
      await i18n.changeLanguage('en') // restore for other tests
    }
  })

  it('surfaces the dual-protocol hint when applicable', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'openrouter-free',
        display_name: 'OpenRouter',
        krouter_provider_name: 'openrouter',
        protocol: 'openai',
        region: 'intl',
        free_type: 'free_tier',
        free_summary: '聚合平台',
        free_quota_usd: 999,
        validity: 'no_expiry',
        conditions: 'email',
        signup_url: 'https://openrouter.ai/keys',
        key_setup_hint: 'OpenAI key',
        last_verified: '2026-05-23',
        user_configured: false,
        additional_protocols: [
          {
            protocol: 'anthropic',
            krouter_provider_name: 'openrouter-anthropic',
            key_setup_hint: '同一个 key, baseURL /v1',
            user_configured: false,
          },
        ],
      },
    ])
    renderWithProviders(<FreeTokens />)
    await waitFor(() => screen.getByText('OpenRouter'))
    // "Also supports" / "也支持" appears in the per-row hint panel.
    expect(screen.getAllByText(/Also supports|也支持/).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('openrouter-anthropic')).toBeInTheDocument()
  })
})
