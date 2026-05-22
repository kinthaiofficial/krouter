import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, act } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import SubscriptionQuotaCard from '../components/SubscriptionQuotaCard'

type Handler = () => unknown
const handlers = new Map<string, Handler>()
const calls: Array<{ method: string; path: string }> = []

// MockEventSource captures addEventListener registrations so tests can
// synthesise SSE events without a real network connection. Each instance
// records itself in the global `mockEventSources` array so test code can
// reach in and dispatch.
const mockEventSources: MockEventSource[] = []
class MockEventSource {
  url: string
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  closed = false
  constructor(url: string) {
    this.url = url
    mockEventSources.push(this)
  }
  addEventListener(type: string, listener: (e: MessageEvent) => void) {
    (this.listeners[type] ??= []).push(listener)
  }
  close() { this.closed = true }
  dispatch(type: string, payload: unknown) {
    const event = { data: JSON.stringify(payload) } as MessageEvent
    for (const l of this.listeners[type] ?? []) l(event)
  }
}

beforeEach(() => {
  handlers.clear()
  calls.length = 0
  mockEventSources.length = 0
  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    const path = url.split('?')[0]
    calls.push({ method: init?.method ?? 'GET', path })
    const body = handlers.get(path)?.() ?? []
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
  vi.stubGlobal('EventSource', MockEventSource)
})

describe('<SubscriptionQuotaCard>', () => {
  it('collapses to nothing when daemon has no polled providers', async () => {
    handlers.set('/internal/subscription/status', () => [])

    const { container } = renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => {
      // give the query time to resolve
      expect(calls.find((c) => c.path === '/internal/subscription/status')).toBeTruthy()
    })
    expect(container.querySelector('[data-testid="subscription-quota-card"]')).toBeNull()
  })

  it('renders tiers with remaining/total + effective cost', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax',
      source_agent: 'openclaw',
      oauth_present: true,
      last_polled_at: new Date(Date.now() - 12 * 60_000).toISOString(),
      tiers: [
        {
          tier_name: 'MiniMax-M*',
          total: 1500, used: 21, remaining: 1479, highspeed: false,
          window_start: new Date(Date.now() - 30 * 60_000).toISOString(),
          window_end: new Date(Date.now() + 4 * 60 * 60_000).toISOString(),
          seconds_to_reset: 14400,
          // Pricing values come from storage.SubscriptionQuota helpers;
          // see internal/storage/subscription_quota.go for the formula.
          // The exact numbers used here just need to round-trip through
          // the UI — display formatting is what's under test.
          effective_cost_per_call_usd: 0.0000313,
          monthly_price_cny: 49,
          monthly_price_usd: 6.76,
        },
      ],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => {
      expect(screen.getByText('Subscription Quota')).toBeInTheDocument()
    })

    expect(screen.getByText('minimax')).toBeInTheDocument()
    expect(screen.getByText(/via openclaw/)).toBeInTheDocument()
    expect(screen.getByText('MiniMax-M*')).toBeInTheDocument()
    expect(screen.getByText(/1,479 \/ 1,500 left/)).toBeInTheDocument()
    expect(screen.getByText(/¥49\/mo \(≈ \$6\.76\)/)).toBeInTheDocument()
  })

  it('shows static-key warning when oauth_present is false', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax',
      oauth_present: false,
      tiers: [],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => {
      expect(screen.getByText(/Static key — no quota data/)).toBeInTheDocument()
    })
  })

  it('invokes refresh when the button is clicked', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax', oauth_present: true, tiers: [],
    }])
    handlers.set('/internal/subscription/refresh', () => [{
      provider: 'minimax', oauth_present: true, tiers: [],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => screen.getByText('Subscription Quota'))

    fireEvent.click(screen.getByRole('button', { name: /refresh/i }))
    await waitFor(() => {
      expect(calls.find((c) => c.method === 'POST' && c.path === '/internal/subscription/refresh')).toBeTruthy()
    })
  })

  it('formats reset time in hours and minutes', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax', oauth_present: true,
      tiers: [{
        tier_name: 'MiniMax-M*',
        total: 1500, used: 0, remaining: 1500, highspeed: false,
        window_start: new Date().toISOString(),
        window_end: new Date().toISOString(),
        seconds_to_reset: 4 * 3600 + 28 * 60,   // 4h 28m
        effective_cost_per_call_usd: 0,
        monthly_price_usd: 0,
      }],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => {
      expect(screen.getByText(/resets in 4h 28m/)).toBeInTheDocument()
    })
  })

  it('shows a banner when a subscription_exhausted SSE event arrives', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax', oauth_present: true,
      tiers: [{
        tier_name: 'MiniMax-M*',
        total: 1500, used: 1500, remaining: 0, highspeed: false,
        window_start: new Date().toISOString(),
        window_end: new Date(Date.now() + 3 * 3600_000).toISOString(),
        seconds_to_reset: 3 * 3600,
        effective_cost_per_call_usd: 0.0000313,
        monthly_price_cny: 49,
        monthly_price_usd: 6.76,
      }],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => screen.getByText('Subscription Quota'))

    // EventSource should have been opened to /internal/events.
    expect(mockEventSources).toHaveLength(1)
    expect(mockEventSources[0].url).toContain('/internal/events')

    // Dispatch the exhaustion event; banner should appear.
    act(() => {
      mockEventSources[0].dispatch('subscription_exhausted', {
        provider: 'minimax',
        tier: 'MiniMax-M*',
        highspeed: false,
        window_end: new Date(Date.now() + 3 * 3600_000).toISOString(),
      })
    })

    await waitFor(() => {
      expect(screen.getByTestId('subscription-exhausted-banner')).toBeInTheDocument()
    })
    expect(screen.getByText(/MiniMax-M\* quota exhausted/)).toBeInTheDocument()
    expect(screen.getByText(/per-token vendors/)).toBeInTheDocument()
  })

  it('dismisses the exhaust banner when the Dismiss button is clicked', async () => {
    handlers.set('/internal/subscription/status', () => [{
      provider: 'minimax', oauth_present: true,
      tiers: [{
        tier_name: 'MiniMax-M*',
        total: 1500, used: 1500, remaining: 0, highspeed: false,
        window_start: new Date().toISOString(),
        window_end: new Date().toISOString(),
        seconds_to_reset: 0,
        effective_cost_per_call_usd: 0,
        monthly_price_cny: 0,
        monthly_price_usd: 0,
      }],
    }])

    renderWithProviders(<SubscriptionQuotaCard />)
    await waitFor(() => screen.getByText('Subscription Quota'))

    act(() => {
      mockEventSources[0].dispatch('subscription_exhausted', {
        provider: 'minimax', tier: 'MiniMax-M*',
        highspeed: false, window_end: new Date().toISOString(),
      })
    })

    const banner = await screen.findByTestId('subscription-exhausted-banner')
    expect(banner).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    await waitFor(() => {
      expect(screen.queryByTestId('subscription-exhausted-banner')).not.toBeInTheDocument()
    })
  })
})
