import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import FreeProvidersCard from '../components/FreeProvidersCard'

type Handler = () => unknown
const handlers = new Map<string, Handler>()

beforeEach(() => {
  handlers.clear()
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    const path = url.split('?')[0]
    const body = handlers.get(path)?.() ?? []
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
})

describe('<FreeProvidersCard>', () => {
  it('collapses to nothing when catalog is empty', async () => {
    handlers.set('/internal/free-providers', () => [])
    const { container } = renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => {
      // Card should not render — no testid in DOM.
      expect(container.querySelector('[data-testid="free-providers-card"]')).toBeNull()
    })
  })

  it('lists available (not yet configured) providers with signup link', async () => {
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

    renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => {
      expect(screen.getByText('DeepSeek')).toBeInTheDocument()
    })
    expect(screen.getByText(/新用户赠送 ¥10/)).toBeInTheDocument()

    // Signup CTA points at the vendor URL.
    const link = screen.getByRole('link', { name: /去申请/i })
    expect(link).toHaveAttribute('href', 'https://platform.deepseek.com/sign_up')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('shows configured badge + hides configured list under collapsed section', async () => {
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

    renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => {
      expect(screen.getByText(/1 already configured/)).toBeInTheDocument()
    })

    // Configured list collapsed by default → DeepSeek not visible yet.
    expect(screen.queryByText('DeepSeek')).not.toBeInTheDocument()

    // Click to expand.
    fireEvent.click(screen.getByText(/1 already configured/))
    await waitFor(() => {
      expect(screen.getByText('DeepSeek')).toBeInTheDocument()
    })
    expect(screen.getByText(/via openclaw/)).toBeInTheDocument()
  })

  it('surfaces exhausted badge when daemon marked the provider down', async () => {
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
        user_configured: false,
        exhausted: true,
        exhausted_reason: 'HTTP 402 — provider reports no credit remaining',
      },
    ])

    renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => {
      expect(screen.getByText(/exhausted/)).toBeInTheDocument()
    })
    expect(screen.getByText(/HTTP 402/)).toBeInTheDocument()
  })

  it('renders dual-protocol hint with alternate krouter_provider_name', async () => {
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
        key_setup_hint: 'OpenAI provider key',
        last_verified: '2026-05-23',
        user_configured: true,
        source_agent: 'openclaw',
        additional_protocols: [
          {
            protocol: 'anthropic',
            krouter_provider_name: 'openrouter-anthropic',
            key_setup_hint: '同一个 key, baseURL /v1, Anthropic 协议',
            user_configured: false,
          },
        ],
      },
    ])

    renderWithProviders(<FreeProvidersCard />)
    // OpenRouter is "configured" so it's in the collapsed section — expand it.
    await waitFor(() => screen.getByText(/1 already configured/))
    fireEvent.click(screen.getByText(/1 already configured/))
    await waitFor(() => screen.getByText('OpenRouter'))

    // Multiple matches expected: top-of-card explainer + per-row hint.
    expect(screen.getAllByText(/也支持/i).length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('openrouter-anthropic')).toBeInTheDocument()
    expect(screen.getByText(/同一个 key, baseURL/)).toBeInTheDocument()
    expect(screen.getByText(/未配置/)).toBeInTheDocument()
  })

  it('dual-protocol entry shows configured badge when alternate is also inherited', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'openrouter-free',
        display_name: 'OpenRouter',
        krouter_provider_name: 'openrouter',
        protocol: 'openai',
        region: 'intl',
        free_type: 'free_tier',
        free_summary: '',
        free_quota_usd: 999,
        validity: '',
        conditions: '',
        signup_url: 'https://openrouter.ai/keys',
        key_setup_hint: '',
        last_verified: '2026-05-23',
        user_configured: true,
        source_agent: 'openclaw',
        additional_protocols: [
          {
            protocol: 'anthropic',
            krouter_provider_name: 'openrouter-anthropic',
            user_configured: true,
            source_agent: 'openclaw',
          },
        ],
      },
    ])

    renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => screen.getByText(/1 already configured/))
    fireEvent.click(screen.getByText(/1 already configured/))
    await waitFor(() => screen.getByText('OpenRouter'))

    // The alternate's "via openclaw" badge.
    expect(screen.getAllByText(/via openclaw/).length).toBeGreaterThanOrEqual(2)
  })

  it('uses different region badge colours for china vs intl', async () => {
    handlers.set('/internal/free-providers', () => [
      {
        id: 'deepseek', display_name: 'DeepSeek', krouter_provider_name: 'deepseek',
        protocol: 'openai', region: 'china', free_type: 'trial_credit',
        free_summary: '', free_quota_usd: 0, validity: '', conditions: '',
        signup_url: 'https://example.com/', key_setup_hint: '',
        last_verified: '2026-05-23', user_configured: false,
      },
      {
        id: 'groq', display_name: 'Groq', krouter_provider_name: 'groq',
        protocol: 'openai', region: 'intl', free_type: 'daily_quota',
        free_summary: '', free_quota_usd: 0, validity: '', conditions: '',
        signup_url: 'https://example.com/', key_setup_hint: '',
        last_verified: '2026-05-23', user_configured: false,
      },
    ])

    renderWithProviders(<FreeProvidersCard />)
    await waitFor(() => {
      expect(screen.getByText('DeepSeek')).toBeInTheDocument()
      expect(screen.getByText('Groq')).toBeInTheDocument()
    })
    expect(screen.getByText('国内')).toBeInTheDocument()
    expect(screen.getByText("INT'L")).toBeInTheDocument()
  })
})
