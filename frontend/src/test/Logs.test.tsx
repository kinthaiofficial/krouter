import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n'
import Logs from '../pages/Logs'
import type { LogRecord } from '../api/client'

class FakeEventSource {
  url = ''
  addEventListener() {}
  close() {}
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(url: string) { this.url = url }
}

function makeRec(over: Partial<LogRecord> = {}): LogRecord {
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
    cached_tokens: 100,
    cost_usd: 0.0012,
    latency_ms: 1842,
    status_code: 200,
    ...over,
  }
}

let lastRecords: LogRecord[] = []

beforeEach(() => {
  lastRecords = []
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    const path = url.split('?')[0]
    let body: unknown = []
    if (path === '/internal/agents') {
      body = [{ name: 'openclaw' }, { name: 'cursor' }]
    } else if (path === '/internal/logs' || path === '/internal/logs/range') {
      body = lastRecords
    }
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
})

describe('Logs page', () => {
  it('renders one expandable row per record after data loads', async () => {
    lastRecords = [
      makeRec({ id: 'r1', requested_model: 'claude-sonnet-4', model: 'glm-4.6' }),
      makeRec({ id: 'r2', requested_model: 'claude-haiku-4-5', model: 'claude-haiku-4-5', agent: 'cursor', provider: 'anthropic' }),
    ]
    renderWithProviders(<Logs />)
    await waitFor(() => {
      // One-line summary per record. The routed model appears in font-mono.
      expect(screen.getAllByText(/glm-4.6/).length).toBeGreaterThan(0)
      expect(screen.getAllByText(/claude-haiku-4-5/).length).toBeGreaterThan(0)
    })
  })

  it('expanding a row reveals the full DecisionCard with cached tokens', async () => {
    lastRecords = [
      makeRec({ id: 'r1', requested_model: 'sonnet-4', model: 'glm-4.6', cached_tokens: 555 }),
    ]
    renderWithProviders(<Logs />)
    await waitFor(() => screen.getAllByText(/glm-4.6/))

    // Tokens breakdown text not visible before expand.
    expect(screen.queryByText(/555/)).not.toBeInTheDocument()

    fireEvent.click(screen.getAllByText(/sonnet-4/)[0])

    await waitFor(() => {
      // After expand the breakdown line "1,245 in · 387 out · 555 cached" is visible.
      expect(screen.getByText(/555/)).toBeInTheDocument()
    })
  })

  it('search filters by requested model, routed model, provider, agent, or id', async () => {
    lastRecords = [
      makeRec({ id: 'r1', requested_model: 'haiku-original', model: 'glm-4.6' }),
      makeRec({ id: 'r2', requested_model: 'opus-4', model: 'claude-sonnet-4' }),
    ]
    renderWithProviders(<Logs />)
    await waitFor(() => screen.getAllByText(/glm-4.6/))

    fireEvent.change(screen.getByPlaceholderText(/Search/i), { target: { value: 'haiku' } })

    await waitFor(() => {
      // r1 matches via requested_model; r2 should disappear.
      expect(screen.getAllByText(/haiku-original/).length).toBeGreaterThan(0)
      expect(screen.queryByText(/opus-4/)).not.toBeInTheDocument()
    })
  })

  it('protocol filter narrows by protocol field', async () => {
    lastRecords = [
      makeRec({ id: 'r1', protocol: 'anthropic', requested_model: 'sonnet', model: 'sonnet' }),
      makeRec({ id: 'r2', protocol: 'openai', requested_model: 'gpt-x', model: 'gpt-x' }),
    ]
    renderWithProviders(<Logs />)
    await waitFor(() => screen.getAllByText(/sonnet/))

    // Find the protocol select by one of its option labels. getAllByRole
    // returns *all* selects in document order; the protocol one is the
    // one that contains the "OpenAI" option in its DOM children.
    const selects = screen.getAllByRole('combobox') as HTMLSelectElement[]
    const protocolSelect = selects.find((el) =>
      Array.from(el.options).some((o) => o.value === 'openai'),
    )
    expect(protocolSelect).toBeTruthy()
    fireEvent.change(protocolSelect!, { target: { value: 'openai' } })

    await waitFor(() => {
      expect(screen.queryByText('sonnet')).not.toBeInTheDocument()
      expect(screen.getAllByText('gpt-x').length).toBeGreaterThan(0)
    })
  })

  it('shows the empty state when there are no records', async () => {
    lastRecords = []
    renderWithProviders(<Logs />)
    await waitFor(() => {
      expect(screen.getByText(/No records found|暂无记录/)).toBeInTheDocument()
    })
  })

  it('Export CSV button is present', async () => {
    lastRecords = [makeRec()]
    renderWithProviders(<Logs />)
    await waitFor(() => screen.getAllByText(/glm-4.6/))
    expect(screen.getByRole('button', { name: /Export CSV|导出 CSV/ })).toBeInTheDocument()
  })
})
