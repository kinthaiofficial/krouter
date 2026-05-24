import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n'
import About from '../pages/About'

interface UpdateStatus {
  current: string
  latest?: string | null
  is_critical?: boolean
  release_notes_url?: string
}

const mockStatus = {
  status: 'ok', version: 'v1.2.3',
  uptime_seconds: 3700, pid: 1234,
  proxy_port: 8402, mgmt_port: 8403,
}
const noUpdate: UpdateStatus = { current: 'v1.2.3', latest: null }
const hasUpdate: UpdateStatus = {
  current: 'v1.2.3',
  latest: 'v1.3.0',
  is_critical: false,
  release_notes_url: 'https://example.com/releases',
}

interface FetchOpts {
  updateResult?: UpdateStatus
  checkFails?: boolean
  delayCheckMs?: number
}

function setupFetch({ updateResult = noUpdate, checkFails = false, delayCheckMs = 0 }: FetchOpts = {}) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string, opts?: RequestInit) => {
    if (url.includes('/internal/update-check')) {
      if (checkFails) return Promise.reject(new Error('network'))
      const respond = () => ({ ok: true, status: 200, json: () => Promise.resolve(updateResult) } as Response)
      return delayCheckMs > 0
        ? new Promise<Response>((res) => setTimeout(() => res(respond()), delayCheckMs))
        : Promise.resolve(respond())
    }
    if (url.includes('/internal/update-apply') && opts?.method === 'POST') {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'applying' }) } as Response)
    }
    if (url.includes('/internal/status')) {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(mockStatus) } as Response)
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) } as Response)
  }))
}

describe('About page (auto-check on open)', () => {
  beforeEach(() => setupFetch())

  it('shows the spinner immediately on mount, then the result', async () => {
    setupFetch({ updateResult: noUpdate, delayCheckMs: 80 })
    renderWithProviders(<About />)
    // Mutation fires inside useEffect on first paint → spinner shows once
    // isPending flips. findByText polls until it appears (or 1 s timeout).
    expect(await screen.findByText(/Checking for updates|正在检查新版本/)).toBeInTheDocument()
    // After resolution → up-to-date state.
    await waitFor(() => {
      expect(screen.getByText(/You're on the latest version|当前已是最新版本/)).toBeInTheDocument()
    })
  })

  it('shows the up-to-date state when latest = null', async () => {
    renderWithProviders(<About />)
    await waitFor(() => {
      expect(screen.getByText(/You're on the latest version|当前已是最新版本/)).toBeInTheDocument()
    })
    // No yellow banner / Apply button.
    expect(screen.queryByText(/Apply Update|立即更新/)).not.toBeInTheDocument()
  })

  it('shows the Apply Update banner when an update is available', async () => {
    setupFetch({ updateResult: hasUpdate })
    renderWithProviders(<About />)
    await waitFor(() => {
      expect(screen.getByText(/Apply Update|立即更新/)).toBeInTheDocument()
    })
    expect(screen.getAllByText(/v1\.3\.0/).length).toBeGreaterThan(0)
  })

  it('Apply Update posts /internal/update-apply', async () => {
    setupFetch({ updateResult: hasUpdate })
    renderWithProviders(<About />)
    await waitFor(() => screen.getByText(/Apply Update|立即更新/))
    fireEvent.click(screen.getByText(/Apply Update|立即更新/))
    await waitFor(() => {
      const calls = (fetch as ReturnType<typeof vi.fn>).mock.calls
      const applyCall = calls.find(([url, opts]) =>
        url.includes('/internal/update-apply') && opts?.method === 'POST')
      expect(applyCall).toBeTruthy()
    })
  })

  it('surfaces a red error block when the check fails', async () => {
    setupFetch({ checkFails: true })
    renderWithProviders(<About />)
    await waitFor(() => {
      expect(screen.getByText(/Couldn't check for updates|检查更新失败/)).toBeInTheDocument()
    })
  })

  it('clicking "Check again" re-runs the check', async () => {
    renderWithProviders(<About />)
    await waitFor(() => screen.getByText(/You're on the latest version|当前已是最新版本/))

    const callsBefore = (fetch as ReturnType<typeof vi.fn>).mock.calls
      .filter(([url]) => url.includes('/internal/update-check')).length

    fireEvent.click(screen.getByText(/Check again|再次检查/))

    await waitFor(() => {
      const callsAfter = (fetch as ReturnType<typeof vi.fn>).mock.calls
        .filter(([url]) => url.includes('/internal/update-check')).length
      expect(callsAfter).toBeGreaterThan(callsBefore)
    })
  })
})
