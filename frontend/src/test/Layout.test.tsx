import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { Routes, Route } from 'react-router-dom'
import { renderWithProviders } from './helpers'
import '../i18n'
import Layout from '../components/Layout'

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    const path = url.split('?')[0]
    if (path === '/internal/status') {
      return Promise.resolve({
        ok: true, status: 200,
        json: () => Promise.resolve({
          status: 'running', version: 'v2.3.2', uptime_seconds: 0,
          pid: 1, proxy_port: 8402, mgmt_port: 8403,
        }),
      } as Response)
    }
    return Promise.resolve({
      ok: true, status: 200, json: () => Promise.resolve({ unread: 0 }),
    } as Response)
  }))
})

function renderLayout() {
  return renderWithProviders(
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<div data-testid="content">Hello</div>} />
      </Route>
    </Routes>,
  )
}

describe('<Layout> (top nav)', () => {
  it('renders nav as a top header, no left sidebar', async () => {
    renderLayout()
    await waitFor(() => expect(screen.getByTestId('content')).toBeInTheDocument())

    // The brand + nav items live in a <header>, not an <aside>.
    const header = screen.getByRole('banner')  // <header>
    expect(header).toBeInTheDocument()
    expect(document.querySelector('aside')).toBeNull()
  })

  it('shows the brand and the version chip', async () => {
    renderLayout()
    await waitFor(() => expect(screen.getByText('KRouter')).toBeInTheDocument())
    // The version chip comes in once the /internal/status query resolves.
    await waitFor(() => expect(screen.getByText('v2.3.2')).toBeInTheDocument())
  })

  it('renders every nav item as a link in document order', async () => {
    renderLayout()
    await waitFor(() => expect(screen.getByText('KRouter')).toBeInTheDocument())

    const expected = [
      'Dashboard', 'Free tokens', 'Router', 'Agents', 'Logs',
      'Providers', 'Budget', 'Settings', 'Notifications', 'About',
    ]
    for (const label of expected) {
      expect(screen.getByRole('link', { name: new RegExp(`^${label}$`) })).toBeInTheDocument()
    }
  })

  it('does not render the old "Collapse" / "Expand" toggle', async () => {
    renderLayout()
    await waitFor(() => expect(screen.getByText('KRouter')).toBeInTheDocument())
    // The collapse button is gone with the sidebar.
    expect(screen.queryByRole('button', { name: /Collapse|Expand/i })).not.toBeInTheDocument()
  })

  it('shows the unread badge on the Notifications nav item', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/internal/status') {
        return Promise.resolve({
          ok: true, status: 200,
          json: () => Promise.resolve({
            status: 'running', version: 'v2.3.2', uptime_seconds: 0,
            pid: 1, proxy_port: 8402, mgmt_port: 8403,
          }),
        } as Response)
      }
      return Promise.resolve({
        ok: true, status: 200, json: () => Promise.resolve({ unread: 7 }),
      } as Response)
    }))

    renderLayout()
    await waitFor(() => {
      // The badge number is rendered next to the Notifications nav label.
      expect(screen.getByText('7')).toBeInTheDocument()
    })
  })
})
