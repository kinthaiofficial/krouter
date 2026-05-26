import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, within } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import AppInheritanceSection from '../components/AppInheritanceSection'

// fetchMock dispatches by URL path so a single global fetch can serve multiple
// endpoints in one test. Each call is recorded for later assertion.
type Handler = () => unknown
const handlers = new Map<string, Handler>()
const calls: Array<{ method: string; path: string }> = []

beforeEach(() => {
  handlers.clear()
  calls.length = 0
  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    const path = url.split('?')[0]
    calls.push({ method: init?.method ?? 'GET', path })
    const handler = handlers.get(path)
    const body = handler ? handler() : []
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve(body),
    } as Response)
  }))
})

describe('<AppInheritanceSection>', () => {
  it('renders supported agents joined with configured state', async () => {
    handlers.set('/internal/apps/supported', () => [
      { app_id: 'openclaw', display_name: 'OpenClaw', default_path: '/usr/local/openclaw.json' },
      { app_id: 'claude-code', display_name: 'Claude Code', default_path: '/Users/u/.zshrc' },
    ])
    handlers.set('/internal/apps/configured', () => [
      {
        app_id: 'openclaw', enabled: true, config_path: '/usr/local/openclaw.json',
        inherited_count: 3, last_scanned_at: Date.now() - 60_000,
      },
      // claude-code intentionally absent → "Not configured"
    ])

    renderWithProviders(<AppInheritanceSection />)

    await waitFor(() => {
      expect(screen.getByText('OpenClaw')).toBeInTheDocument()
      expect(screen.getByText('Claude Code')).toBeInTheDocument()
    })

    // OpenClaw row: shows the enabled badge + inherited count.
    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText(/3 providers inherited/)).toBeInTheDocument()

    // Claude Code row: shows "Not configured" since no agent_settings row.
    expect(screen.getByText('Not configured')).toBeInTheDocument()
  })

  it('renders nothing when the daemon binary has no scanners compiled in', async () => {
    handlers.set('/internal/apps/supported', () => [])
    handlers.set('/internal/apps/configured', () => [])

    const { container } = renderWithProviders(<AppInheritanceSection />)

    // Wait for the section to disappear after loading resolves to empty list.
    await waitFor(() => {
      expect(container.querySelector('[data-testid="app-inheritance-section"]')).toBeNull()
    })
  })

  it('calls /enable when user clicks Enable on an unconfigured agent', async () => {
    handlers.set('/internal/apps/supported', () => [
      { app_id: 'openclaw', display_name: 'OpenClaw', default_path: '/p' },
    ])
    handlers.set('/internal/apps/configured', () => [])
    handlers.set('/internal/apps/openclaw/enable', () => ({ ok: true }))

    renderWithProviders(<AppInheritanceSection />)
    await waitFor(() => screen.getByText('OpenClaw'))

    const row = screen.getByText('OpenClaw').closest('li')!
    fireEvent.click(within(row).getByRole('button', { name: /enable/i }))

    await waitFor(() => {
      const enableCall = calls.find(
        (c) => c.method === 'POST' && c.path === '/internal/apps/openclaw/enable',
      )
      expect(enableCall).toBeTruthy()
    })
  })

  it('shows error badge when last_error is set', async () => {
    handlers.set('/internal/apps/supported', () => [
      { app_id: 'openclaw', display_name: 'OpenClaw', default_path: '/p' },
    ])
    handlers.set('/internal/apps/configured', () => [
      {
        app_id: 'openclaw',
        enabled: true,
        config_path: '/wrong/path.json',
        inherited_count: 0,
        last_error: 'parse openclaw config: unexpected token',
        last_scanned_at: Date.now(),
      },
    ])

    renderWithProviders(<AppInheritanceSection />)
    await waitFor(() => {
      expect(screen.getByText(/parse openclaw config/)).toBeInTheDocument()
    })
  })
})
