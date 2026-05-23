import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, act } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n'
import BudgetPage from '../pages/Budget'

class MockEventSource {
  url: string
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  closed = false
  constructor(url: string) { this.url = url }
  addEventListener(type: string, listener: (e: MessageEvent) => void) {
    (this.listeners[type] ??= []).push(listener)
  }
  close() { this.closed = true }
}

interface State {
  settings: { preset: string; language: string; budget_warnings: Record<string, number>; notification_categories: Record<string, boolean> }
  budget: {
    date: string; requests_today: number; cost_today_usd: number; savings_today_usd: number;
    daily_limit_usd?: number; daily_percent_used?: number; budget_blocked?: boolean
  }
  events: Array<{ id: number; ts: string; event_type: string; daily_percent: number; daily_cost_usd: number; daily_limit_usd: number }>
  patches: Array<Record<string, unknown>>
}

let state: State

beforeEach(() => {
  state = {
    settings: {
      preset: 'balanced',
      language: 'en',
      budget_warnings: { daily: 50, weekly: 200 },
      notification_categories: {},
    },
    budget: {
      date: '2026-05-23',
      requests_today: 42,
      cost_today_usd: 25,
      savings_today_usd: 1.23,
      daily_limit_usd: 50,
      daily_percent_used: 0.5,
      budget_blocked: false,
    },
    events: [],
    patches: [],
  }
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: RequestInit) => {
    const path = url.split('?')[0]
    if (opts?.method === 'PATCH' && path === '/internal/settings') {
      const body = JSON.parse(opts.body as string)
      state.patches.push(body)
      if (body.budget_warnings) {
        state.settings.budget_warnings = { ...state.settings.budget_warnings, ...body.budget_warnings }
      }
      return { ok: true, status: 200, json: () => Promise.resolve(state.settings) } as Response
    }
    let body: unknown = {}
    if (path === '/internal/settings') body = state.settings
    else if (path === '/internal/budget') body = state.budget
    else if (path === '/internal/budget/events') body = state.events
    return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
  }))
})

describe('<BudgetPage>', () => {
  it('shows today\'s spend / limit / remaining', async () => {
    renderWithProviders(<BudgetPage />)
    await waitFor(() => {
      // $25.0000 appears twice (spent + remaining = 50 − 25).
      expect(screen.getAllByText(/\$25\.0000/).length).toBeGreaterThanOrEqual(2)
      expect(screen.getByText(/\$50\.00/)).toBeInTheDocument()
    })
  })

  it('marks the state badge "Blocked" when budget_blocked = true', async () => {
    state.budget.budget_blocked = true
    state.budget.daily_percent_used = 1.05
    state.budget.cost_today_usd = 52.5
    renderWithProviders(<BudgetPage />)
    await waitFor(() => {
      // "Blocked" appears in the badge AND in the blocked-hint banner.
      expect(screen.getAllByText(/Blocked|已超限/i).length).toBeGreaterThanOrEqual(1)
    })
  })

  it('renders the events timeline when events exist', async () => {
    state.events = [
      { id: 1, ts: '2026-05-23T10:30:00Z', event_type: 'blocked', daily_percent: 1.0, daily_cost_usd: 50, daily_limit_usd: 50 },
      { id: 2, ts: '2026-05-23T09:15:00Z', event_type: 'warning_95', daily_percent: 0.95, daily_cost_usd: 47.5, daily_limit_usd: 50 },
      { id: 3, ts: '2026-05-23T08:00:00Z', event_type: 'warning_80', daily_percent: 0.80, daily_cost_usd: 40, daily_limit_usd: 50 },
    ]
    renderWithProviders(<BudgetPage />)
    await waitFor(() => {
      expect(screen.getByText(/BLOCKED|已拦截/)).toBeInTheDocument()
      expect(screen.getByText(/95%/)).toBeInTheDocument()
      expect(screen.getByText(/80%/)).toBeInTheDocument()
    })
  })

  it('shows the empty state when there are no events', async () => {
    state.events = []
    renderWithProviders(<BudgetPage />)
    await waitFor(() => {
      expect(screen.getByText(/No threshold events|暂无阈值事件/)).toBeInTheDocument()
    })
  })

  it('typing in the daily limit and blurring sends a PATCH /internal/settings', async () => {
    renderWithProviders(<BudgetPage />)
    // Wait for the inputs to populate from settings.
    await waitFor(() => screen.getByDisplayValue('50'))
    const inputs = screen.getAllByRole('spinbutton') as HTMLInputElement[]
    const daily = inputs[0] // first input is daily
    fireEvent.change(daily, { target: { value: '75' } })
    fireEvent.blur(daily)
    await waitFor(() => {
      expect(state.patches.some((p) =>
        typeof p.budget_warnings === 'object' &&
        p.budget_warnings !== null &&
        (p.budget_warnings as Record<string, unknown>).daily === 75,
      )).toBe(true)
    })
  })

  it('resets the daily limit draft when the user blurs with a negative value', async () => {
    renderWithProviders(<BudgetPage />)
    await waitFor(() => screen.getByDisplayValue('50'))
    const inputs = screen.getAllByRole('spinbutton') as HTMLInputElement[]
    const daily = inputs[0]

    fireEvent.change(daily, { target: { value: '-10' } })
    fireEvent.blur(daily)

    // Draft snaps back to the last known good value (50) — no silent drop.
    await waitFor(() => expect(daily.value).toBe('50'))
    // No PATCH happened for the bad input.
    expect(state.patches).toEqual([])
  })

  it('budget_warning SSE event invalidates the queries (debounced)', async () => {
    const mocks: MockEventSource[] = []
    const Original = MockEventSource
    class TrackingES extends Original {
      constructor(url: string) { super(url); mocks.push(this) }
    }
    vi.stubGlobal('EventSource', TrackingES)

    renderWithProviders(<BudgetPage />)
    await waitFor(() => screen.getByDisplayValue('50'))

    const fetchCallsBefore = (fetch as ReturnType<typeof vi.fn>).mock.calls.length

    // Fire a burst of three events back-to-back. The 500 ms debounce should
    // collapse them into a single (budget + budget/events) refetch pair.
    act(() => {
      for (let i = 0; i < 3; i++) {
        for (const l of mocks[0].listeners['budget_warning'] ?? []) {
          l({ data: '{}' } as MessageEvent)
        }
      }
    })

    await waitFor(() => {
      const after = (fetch as ReturnType<typeof vi.fn>).mock.calls.length
      expect(after).toBeGreaterThan(fetchCallsBefore)
    }, { timeout: 2000 })

    // Bound the refetch count: at most one /internal/budget +
    // one /internal/budget/events refetch from the burst, plus the
    // settings call already issued at mount. We assert <= 4 new calls.
    const newCalls = (fetch as ReturnType<typeof vi.fn>).mock.calls.length - fetchCallsBefore
    expect(newCalls).toBeLessThanOrEqual(4)
  })
})
