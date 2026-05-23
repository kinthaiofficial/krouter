import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
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
          status: 'running', version: 'v2.2.0', uptime_seconds: 0,
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
        <Route path="/" element={<div data-testid="content" />} />
      </Route>
    </Routes>,
  )
}

describe('<Layout>', () => {
  it('does not render the bottom "proxy :8402" footer or its divider', async () => {
    renderLayout()
    await waitFor(() => expect(screen.getByTestId('content')).toBeInTheDocument())
    // The proxy port footer is removed in this PR — text should be absent.
    expect(screen.queryByText(/proxy\s*:?\s*8402/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/代理端口/)).not.toBeInTheDocument()
  })

  it('starts expanded by default and shows labels for nav items', async () => {
    renderLayout()
    await waitFor(() => {
      expect(screen.getByRole('link', { name: /Dashboard/i })).toBeInTheDocument()
    })
    // "Router" nav label is present (text visible).
    expect(screen.getAllByText(/Router|路由/).length).toBeGreaterThan(0)
  })

  it('collapses when the toggle button is clicked, hiding labels', async () => {
    renderLayout()
    await waitFor(() => screen.getByRole('button', { name: /Collapse|折叠/ }))

    // Initially the Dashboard nav label is visible as text.
    const dashboardBefore = screen.getByText(/^Dashboard$|^仪表盘$/)
    expect(dashboardBefore).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /Collapse|折叠/ }))

    // After collapse, the Dashboard label text is no longer rendered.
    await waitFor(() => {
      expect(screen.queryByText(/^Dashboard$|^仪表盘$/)).not.toBeInTheDocument()
    })

    // The toggle button now says "Expand".
    expect(screen.getByRole('button', { name: /Expand|展开/ })).toBeInTheDocument()
  })

  it('persists the collapsed state in localStorage across reloads', async () => {
    const { unmount } = renderLayout()
    await waitFor(() => screen.getByRole('button', { name: /Collapse|折叠/ }))
    fireEvent.click(screen.getByRole('button', { name: /Collapse|折叠/ }))

    await waitFor(() => {
      expect(localStorage.getItem('krouter:sidebar-collapsed')).toBe('1')
    })

    unmount()

    // Second render should pick up the collapsed preference.
    renderLayout()
    await waitFor(() => {
      // Collapsed state → toggle button reads "Expand".
      expect(screen.getByRole('button', { name: /Expand|展开/ })).toBeInTheDocument()
    })
  })

  it('shows a tiny red dot on the collapsed Notifications icon when unread > 0', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/internal/status') {
        return Promise.resolve({
          ok: true, status: 200,
          json: () => Promise.resolve({
            status: 'running', version: 'v2.2.0', uptime_seconds: 0,
            pid: 1, proxy_port: 8402, mgmt_port: 8403,
          }),
        } as Response)
      }
      return Promise.resolve({
        ok: true, status: 200, json: () => Promise.resolve({ unread: 3 }),
      } as Response)
    }))
    localStorage.setItem('krouter:sidebar-collapsed', '1')

    renderLayout()
    await waitFor(() => {
      // aria-label set on the small dot when collapsed.
      expect(screen.getByLabelText('3 unread')).toBeInTheDocument()
    })
  })
})
