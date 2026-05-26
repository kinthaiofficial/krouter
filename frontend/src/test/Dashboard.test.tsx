import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import Dashboard from '../pages/Dashboard'

// Stub EventSource so SSE doesn't throw in jsdom.
class FakeEventSource {
  addEventListener() {}
  close() {}
}
vi.stubGlobal('EventSource', FakeEventSource)

const mockBudget = { date: '2026-05-13', requests_today: 5, cost_today_usd: 0.12, savings_today_usd: 0.08 }
const mockQuota = [{ window: '5h', tokens_used: 1000, window_start: '', window_end: '', updated_at: '' }]
const mockLogs = [
  {
    id: 'log1', ts: new Date().toISOString(), app: 'openclaw',
    protocol: 'anthropic', model: 'claude-haiku-4-5', provider: 'anthropic',
    input_tokens: 100, output_tokens: 50, cost_usd: 0.002, latency_ms: 300, status_code: 200,
  },
]
const mockPreset = { preset: 'balanced' as const }

const mockDashStats = {
  apps_connected: 2,
  weekly: { requests: 100, cost_usd: 1.23, savings_usd: 0.45 },
  providers: [],
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    const body = url.includes('/budget') ? mockBudget
      : url.includes('/quota') ? mockQuota
      : url.includes('/logs') ? mockLogs
      : url.includes('/preset') ? mockPreset
      : url.includes('/dashboard/stats') ? mockDashStats
      : url.includes('/free-providers') ? []
      : url.includes('/subscription') ? []
      : { unread: 0 }
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve(body),
    })
  }))
})

describe('Dashboard', () => {
  it('renders without crash', () => {
    renderWithProviders(<Dashboard />)
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('displays preset switcher', () => {
    renderWithProviders(<Dashboard />)
    expect(screen.getByText('Saver')).toBeInTheDocument()
    expect(screen.getByText('Balanced')).toBeInTheDocument()
    expect(screen.getByText('Quality')).toBeInTheDocument()
  })

  it('shows today stats from /internal/budget', async () => {
    renderWithProviders(<Dashboard />)
    await waitFor(() => {
      expect(screen.getByText('5')).toBeInTheDocument() // requests_today
    })
  })

  it('shows quota bar after data loads', async () => {
    renderWithProviders(<Dashboard />)
    await waitFor(() => {
      expect(screen.getByText('5-Hour Window')).toBeInTheDocument()
    })
  })

  it('renders without crashing when logs are present', () => {
    renderWithProviders(<Dashboard />)
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })
})
