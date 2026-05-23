import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n'
import Providers from '../pages/Providers'
import type { ProviderInfo, ProviderModelRow } from '../api/client'

function makeProvider(over: Partial<ProviderInfo> = {}): ProviderInfo {
  return {
    name: 'anthropic',
    display_name: 'Anthropic',
    protocol: 'anthropic',
    base_url: 'https://api.anthropic.com',
    path_prefix: '',
    is_builtin: true,
    available: true,
    configured: true,
    consecutive_failures: 0,
    success_rate: 1.0,
    requests_today: 12,
    cost_today_usd: 0.42,
    latency_p50_ms: 320,
    latency_p95_ms: 880,
    requests_total: 1500,
    input_tokens_total: 1_245_000,
    output_tokens_total: 387_000,
    cached_tokens_total: 100_000,
    cost_total_usd: 4.27,
    model_count: 8,
    ...over,
  }
}

let providers: ProviderInfo[] = []
let modelsByProvider: Record<string, ProviderModelRow[]> = {}

beforeEach(() => {
  providers = []
  modelsByProvider = {}
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const path = url.split('?')[0]
    if (path === '/internal/providers') {
      return { ok: true, status: 200, json: () => Promise.resolve(providers) } as Response
    }
    const m = /^\/internal\/providers\/([^/]+)\/models$/.exec(path)
    if (m) {
      const provider = decodeURIComponent(m[1])
      return { ok: true, status: 200, json: () => Promise.resolve(modelsByProvider[provider] ?? []) } as Response
    }
    return { ok: true, status: 200, json: () => Promise.resolve([]) } as Response
  }))
})

describe('Providers page', () => {
  it('shows configured providers and the new chip stats', async () => {
    providers = [makeProvider({ name: 'anthropic', requests_total: 1500, cost_total_usd: 4.27, model_count: 8 })]
    renderWithProviders(<Providers />)
    await waitFor(() => expect(screen.getByText('Anthropic')).toBeInTheDocument())

    // Chips render lifetime count, cost, and model count.
    expect(screen.getByText('1,500')).toBeInTheDocument()
    expect(screen.getByText('$4.27')).toBeInTheDocument()
    expect(screen.getByText('8')).toBeInTheDocument()
  })

  it('separates configured (Active) from unconfigured (Not configured)', async () => {
    providers = [
      makeProvider({ name: 'anthropic', configured: true, display_name: 'Anthropic' }),
      makeProvider({
        name: 'deepseek',
        display_name: 'DeepSeek',
        configured: false,
        available: false,
        protocol: 'openai',
        base_url: '',
        requests_total: 0,
        cost_total_usd: 0,
        model_count: 0,
      }),
    ]
    renderWithProviders(<Providers />)
    await waitFor(() => {
      expect(screen.getByText(/Active|运行中/)).toBeInTheDocument()
      expect(screen.getByText(/Not configured|未配置/)).toBeInTheDocument()
    })
    expect(screen.getByText('Anthropic')).toBeInTheDocument()
    expect(screen.getByText('DeepSeek')).toBeInTheDocument()
  })

  it('expanding a card surfaces endpoint details and the model price table', async () => {
    providers = [makeProvider({
      name: 'zai',
      display_name: 'Zhipu',
      protocol: 'openai',
      base_url: 'https://open.bigmodel.cn',
      path_prefix: '/api/paas/v4',
      model_count: 3,
      requests_total: 0,
      cost_total_usd: 0,
    })]
    modelsByProvider['zai'] = [
      { model_id: 'glm-4.6', input_per_mtok: 0.5, output_per_mtok: 1.5, max_tokens: 128000 },
      { model_id: 'glm-4-flash', input_per_mtok: 0, output_per_mtok: 0, max_tokens: 32768 },
    ]
    renderWithProviders(<Providers />)
    await waitFor(() => screen.getByText('Zhipu'))

    // Header summary visible; details not yet.
    expect(screen.queryByText('glm-4.6')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText('Zhipu'))

    await waitFor(() => {
      expect(screen.getByText('glm-4.6')).toBeInTheDocument()
    })
    // The full endpoint shows in both the card header (preview) and in
    // the expanded full_endpoint row — multiple matches expected.
    expect(screen.getAllByText(/https:\/\/open\.bigmodel\.cn\/api\/paas\/v4/).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('$0.50')).toBeInTheDocument() // input price
    expect(screen.getByText('$1.50')).toBeInTheDocument() // output price
    // Free model renders dashes for price.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('shows consecutive failure banner inside the expanded view', async () => {
    providers = [makeProvider({
      name: 'broken',
      display_name: 'Broken',
      configured: true,
      consecutive_failures: 4,
      last_error_code: 503,
      success_rate: 0.2,
    })]
    renderWithProviders(<Providers />)
    await waitFor(() => screen.getByText('Broken'))
    fireEvent.click(screen.getByText('Broken'))
    await waitFor(() => {
      expect(screen.getByText(/4 consecutive failures|连续失败 4 次/)).toBeInTheDocument()
    })
  })

  it('surfaces an explicit error message when the models endpoint 500s', async () => {
    providers = [makeProvider({ name: 'flaky', display_name: 'Flaky', model_count: 0 })]
    // Override fetch only for this test — /providers responds 200 with
    // the provider list, /providers/flaky/models responds 500.
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      const path = url.split('?')[0]
      if (path === '/internal/providers') {
        return { ok: true, status: 200, json: () => Promise.resolve(providers) } as Response
      }
      if (path === '/internal/providers/flaky/models') {
        return { ok: false, status: 500, json: () => Promise.resolve({ error: 'boom' }) } as Response
      }
      return { ok: true, status: 200, json: () => Promise.resolve([]) } as Response
    }))

    renderWithProviders(<Providers />)
    await waitFor(() => screen.getByText('Flaky'))
    fireEvent.click(screen.getByText('Flaky'))

    await waitFor(() => {
      expect(screen.getByText(/Failed to load models|加载该 Provider 的模型失败/)).toBeInTheDocument()
    })
    // Should NOT show the empty-state copy — that would conflate the
    // genuinely-empty case with the error case.
    expect(screen.queryByText(/No models catalogued yet|该 Provider 暂无已收录的模型/)).not.toBeInTheDocument()
  })

  it('renders both base_url and path_prefix as separate rows', async () => {
    providers = [makeProvider({
      name: 'qwen',
      display_name: 'Qwen',
      protocol: 'openai',
      base_url: 'https://dashscope.aliyuncs.com',
      path_prefix: '/compatible-mode/v1',
    })]
    renderWithProviders(<Providers />)
    await waitFor(() => screen.getByText('Qwen'))
    fireEvent.click(screen.getByText('Qwen'))
    await waitFor(() => {
      expect(screen.getByText('https://dashscope.aliyuncs.com')).toBeInTheDocument()
      expect(screen.getByText('/compatible-mode/v1')).toBeInTheDocument()
      // Full endpoint shows twice: header preview + full_endpoint detail row.
      expect(screen.getAllByText('https://dashscope.aliyuncs.com/compatible-mode/v1').length).toBeGreaterThanOrEqual(1)
    })
  })
})
