import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import About from '../pages/About'

const mockStatus = { status: 'ok', version: 'v1.2.3', uptime_seconds: 3700, pid: 1234, proxy_port: 8402, mgmt_port: 8403 }
const mockUpdateNoUpgrade = { current: 'v1.2.3', latest: undefined }
const mockUpdateAvailable = { current: 'v1.2.3', latest: 'v1.3.0', is_critical: false, release_notes_url: 'https://example.com/releases' }

function setupFetch(updateStatus = mockUpdateNoUpgrade) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    const body = url.includes('/update-status') ? updateStatus
      : url.includes('/status') ? mockStatus
      : {}
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
  }))
}

beforeEach(() => setupFetch())

describe('About page', () => {
  it('shows current version', async () => {
    renderWithProviders(<About />)
    await waitFor(() => {
      expect(screen.getByText('v1.2.3')).toBeInTheDocument()
    })
  })

  it('shows uptime in human-readable form', async () => {
    renderWithProviders(<About />)
    await waitFor(() => {
      // 3700s = 1h 1m
      expect(screen.getByText(/Uptime:/)).toBeInTheDocument()
    })
  })

  it('does not show update banner when no update', async () => {
    renderWithProviders(<About />)
    await waitFor(() => screen.getByText('v1.2.3'))
    expect(screen.queryByText(/New version available/)).not.toBeInTheDocument()
  })

  it('shows update banner when update is available', async () => {
    setupFetch(mockUpdateAvailable)
    renderWithProviders(<About />)
    await waitFor(() => {
      expect(screen.getByText('New version available')).toBeInTheDocument()
      // Version appears in multiple places (badge + text) — just verify at least one.
      expect(screen.getAllByText(/v1\.3\.0/).length).toBeGreaterThan(0)
    })
  })

  it('Apply Update button calls POST /internal/update-apply', async () => {
    setupFetch(mockUpdateAvailable)
    renderWithProviders(<About />)
    await waitFor(() => screen.getByText('Apply Update'))
    fireEvent.click(screen.getByText('Apply Update'))
    await waitFor(() => {
      const calls = (fetch as ReturnType<typeof vi.fn>).mock.calls
      const applyCall = calls.find(([url, opts]) => url.includes('/update-apply') && opts?.method === 'POST')
      expect(applyCall).toBeTruthy()
    })
  })
})
