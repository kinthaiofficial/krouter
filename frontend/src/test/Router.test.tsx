import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, act } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n' // initialise translations so t() returns real strings, not key fallbacks
import Router from '../pages/Router'

type Handler = () => unknown
const handlers = new Map<string, Handler>()

const mockEventSources: MockEventSource[] = []
class MockEventSource {
  url: string
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
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

function makeRec(over: Partial<Record<string, unknown>> = {}) {
  return {
    id: 'req_default',
    ts: '2026-05-23T10:00:00Z',
    agent: 'openclaw',
    protocol: 'anthropic',
    requested_model: 'claude-sonnet-4',
    model: 'glm-4.6',
    provider: 'zai',
    input_tokens: 1245,
    output_tokens: 387,
    cost_usd: 0.0012,
    latency_ms: 1842,
    status_code: 200,
    ...over,
  }
}

beforeEach(() => {
  handlers.clear()
  mockEventSources.length = 0
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    const path = url.split('?')[0]
    const body = handlers.get(path)?.() ?? []
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
  vi.stubGlobal('EventSource', MockEventSource)
})

describe('<Router>', () => {
  it('shows the empty-state when seed and SSE are both empty', async () => {
    handlers.set('/internal/logs', () => [])
    renderWithProviders(<Router />)
    await waitFor(() => {
      expect(screen.getByText(/Waiting for the first request|等待第一个请求/i)).toBeInTheDocument()
    })
  })

  it('renders the seeded latest record with requested → routed diff', async () => {
    handlers.set('/internal/logs', () => [
      makeRec({
        id: 'req_seed_1',
        requested_model: 'claude-sonnet-4',
        model: 'glm-4.6',
        provider: 'zai',
      }),
    ])
    renderWithProviders(<Router />)

    await waitFor(() => {
      expect(screen.getByText(/LATEST|最新/)).toBeInTheDocument()
    })

    // Both sides of the diff show the model names.
    expect(screen.getByText('claude-sonnet-4')).toBeInTheDocument()
    expect(screen.getByText('glm-4.6')).toBeInTheDocument()
    expect(screen.getByText('zai')).toBeInTheDocument()

    // Cost shown
    expect(screen.getByText('$0.0012')).toBeInTheDocument()
  })

  it('does not render the diff side panel when no change happened', async () => {
    handlers.set('/internal/logs', () => [
      makeRec({
        id: 'req_seed_unchanged',
        requested_model: 'glm-4.6',
        model: 'glm-4.6',
      }),
    ])
    renderWithProviders(<Router />)
    await waitFor(() => {
      expect(screen.getByText(/routed unchanged|未做模型转换/i)).toBeInTheDocument()
    })
  })

  it('promotes a new SSE event to be the LATEST card', async () => {
    handlers.set('/internal/logs', () => [
      makeRec({ id: 'req_old', requested_model: 'sonnet-old', model: 'sonnet-old' }),
    ])
    renderWithProviders(<Router />)
    await waitFor(() => {
      // Both the requested and routed panes render the model name, so we
      // expect at least one match (could be two when requested === routed).
      expect(screen.getAllByText('sonnet-old').length).toBeGreaterThan(0)
    })

    // Push a fresh routing decision over SSE.
    act(() => {
      mockEventSources[0].dispatch('request_completed', makeRec({
        id: 'req_new',
        requested_model: 'claude-sonnet-4',
        model: 'glm-4.6',
      }))
    })

    // New record now occupies the latest card.
    await waitFor(() => {
      expect(screen.getByText('claude-sonnet-4')).toBeInTheDocument()
    })
  })

  it('collapses older records under "N earlier" and expands on click', async () => {
    handlers.set('/internal/logs', () => [
      makeRec({ id: 'req_a', requested_model: 'a', model: 'A' }),
      makeRec({ id: 'req_b', requested_model: 'older-model', model: 'older-routed' }),
      makeRec({ id: 'req_c', requested_model: 'oldest', model: 'oldest-routed' }),
    ])
    renderWithProviders(<Router />)

    await waitFor(() => {
      expect(screen.getByText(/2 earlier|再显示 2 条/)).toBeInTheDocument()
    })

    // Older records are not yet in the DOM (collapsed).
    expect(screen.queryByText('older-model')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText(/2 earlier|再显示 2 条/))

    await waitFor(() => {
      expect(screen.getByText('older-model')).toBeInTheDocument()
      expect(screen.getByText('oldest')).toBeInTheDocument()
    })
  })

  it('deduplicates the SSE event if its id is already in the feed', async () => {
    handlers.set('/internal/logs', () => [
      makeRec({ id: 'req_dup', requested_model: 'm1', model: 'm1' }),
    ])
    renderWithProviders(<Router />)
    await waitFor(() =>
      expect(screen.getAllByText('m1').length).toBeGreaterThan(0),
    )

    act(() => {
      mockEventSources[0].dispatch('request_completed', makeRec({
        id: 'req_dup', requested_model: 'm1', model: 'm1',
      }))
    })

    // No "N earlier" pill should appear because the duplicate didn't add a row.
    expect(screen.queryByText(/earlier|再显示/)).not.toBeInTheDocument()
  })

  it('renders a tooltip and inline explanation for non-2xx status codes', async () => {
    // 402 = Payment Required. The latest card should:
    //   (a) attach a title="402 — Payment required …" attribute on the pill
    //   (b) render an inline explanation block below the latency line
    handlers.set('/internal/logs', () => [
      makeRec({
        id: 'req_402',
        requested_model: 'claude-sonnet-4',
        model: 'glm-4.6',
        status_code: 402,
      }),
    ])
    renderWithProviders(<Router />)
    await waitFor(() => screen.getByText(/LATEST|最新/))

    // Tooltip — the `title` attribute on the pill contains both the
    // code and the explanation.
    const pill = screen.getByText('402').closest('span')
    expect(pill).toBeTruthy()
    expect(pill!.getAttribute('title')).toMatch(/Payment required|付费/)

    // Inline explanation below the diff body.
    expect(screen.getByText(/HTTP 402/)).toBeInTheDocument()
  })
})
