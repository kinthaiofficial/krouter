import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { render } from '@testing-library/react'
import ShellStep from '../pages/ShellStep'

const FAST = { maxAttempts: 2, pollIntervalMs: 0 }

function mockFetch({
  shellOk = true,
  shellError = '',
  daemonReady = false,
  daemonRedirect = 'http://127.0.0.1:8403/krouter/',
  finalizeOk = true,
} = {}) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    const u = url as string
    if (u.includes('/shell-integration')) {
      if (!shellOk) {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({ error: shellError || 'shell error' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ok: true }) })
    }
    if (u.includes('/daemon-ready')) {
      return Promise.resolve({
        ok: true, status: 200,
        json: () => Promise.resolve(daemonReady ? { ready: true, redirect_url: daemonRedirect } : { ready: false }),
      })
    }
    if (u.includes('/finalize')) {
      if (!finalizeOk) {
        return Promise.resolve({ ok: false, status: 410, json: () => Promise.resolve({ error: 'already finalized' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) })
  }))
}

beforeEach(() => {
  Object.defineProperty(window, 'location', {
    value: { href: '' },
    configurable: true,
    writable: true,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ShellStep — initial render', () => {
  it('shows Skip and Apply buttons', () => {
    render(<ShellStep onNext={() => {}} {...FAST} />)
    expect(screen.getByText('Skip')).toBeInTheDocument()
    expect(screen.getByText('Apply')).toBeInTheDocument()
    expect(screen.queryByText('Open KRouter Dashboard →')).not.toBeInTheDocument()
  })

  it('does not show error or spinner initially', () => {
    render(<ShellStep onNext={() => {}} {...FAST} />)
    expect(screen.queryByText(/Starting KRouter/)).not.toBeInTheDocument()
    expect(screen.queryByText(/took too long/)).not.toBeInTheDocument()
  })
})

describe('ShellStep — Skip', () => {
  it('Skip calls onNext without any API request', async () => {
    const onNext = vi.fn()
    vi.stubGlobal('fetch', vi.fn())
    render(<ShellStep onNext={onNext} {...FAST} />)
    fireEvent.click(screen.getByText('Skip'))
    expect(onNext).toHaveBeenCalledOnce()
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('ShellStep — Apply: success', () => {
  it('shows Applying… while request is in-flight', async () => {
    let resolve!: (v: unknown) => void
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(r => { resolve = r })))
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    expect(await screen.findByText('Applying…')).toBeInTheDocument()
    resolve({ ok: true, status: 200, json: () => Promise.resolve({ ok: true }) })
  })

  it('shows success banner and Open Dashboard button after apply', async () => {
    mockFetch({ shellOk: true })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => {
      expect(screen.getByText(/Shell integration applied/)).toBeInTheDocument()
    })
    expect(screen.getByText('Open KRouter Dashboard →')).toBeInTheDocument()
    expect(screen.queryByText('Apply')).not.toBeInTheDocument()
    expect(screen.queryByText('Skip')).not.toBeInTheDocument()
  })
})

describe('ShellStep — Apply: error', () => {
  it('shows error message when shell integration API fails', async () => {
    mockFetch({ shellOk: false, shellError: 'permission denied' })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => {
      expect(screen.getByText('permission denied')).toBeInTheDocument()
    })
    // Buttons reappear so user can retry
    expect(screen.getByText('Apply')).toBeInTheDocument()
    expect(screen.getByText('Skip')).toBeInTheDocument()
  })

  it('clears error and retries on second Apply click', async () => {
    let attempt = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/shell-integration')) {
        attempt++
        if (attempt === 1) {
          return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({ error: 'tmp error' }) })
        }
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ok: true }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) })
    }))
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('tmp error'))
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => {
      expect(screen.getByText(/Shell integration applied/)).toBeInTheDocument()
    })
    expect(screen.queryByText('tmp error')).not.toBeInTheDocument()
  })
})

describe('ShellStep — Open Dashboard: happy path', () => {
  it('shows spinner after clicking Open Dashboard', async () => {
    mockFetch({ shellOk: true, daemonReady: false })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    expect(await screen.findByText('Starting KRouter daemon…')).toBeInTheDocument()
  })

  it('calls finalize before polling', async () => {
    const fetches: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      fetches.push(url as string)
      if ((url as string).includes('/shell-integration')) {
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ok: true }) })
      }
      if ((url as string).includes('/finalize')) {
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' }) })
    }))
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
    const finalizeIdx = fetches.findIndex(u => u.includes('/finalize'))
    const daemonIdx = fetches.findIndex(u => u.includes('/daemon-ready'))
    expect(finalizeIdx).toBeGreaterThanOrEqual(0)
    expect(daemonIdx).toBeGreaterThan(finalizeIdx)
  })

  it('navigates to the redirect_url from daemon-ready', async () => {
    mockFetch({ shellOk: true, daemonReady: true, daemonRedirect: 'http://127.0.0.1:8403/krouter/' })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })
})

describe('ShellStep — Open Dashboard: error cases', () => {
  it('shows timeout error with /krouter/ URL when daemon never starts', async () => {
    mockFetch({ shellOk: true, daemonReady: false })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
    expect(screen.getByText(/8403\/krouter\//)).toBeInTheDocument()
  })

  it('shows timeout error when all daemon-ready polls throw', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/shell-integration') || (url as string).includes('/finalize')) {
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ok: true }) })
      }
      return Promise.reject(new Error('connection refused'))
    }))
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
    expect(screen.getByText(/8403\/krouter\//)).toBeInTheDocument()
  })

  it('Open Dashboard button reappears after timeout for retry', async () => {
    mockFetch({ shellOk: true, daemonReady: false })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => screen.getByText(/KRouter took too long/))
    expect(screen.getByText('Open KRouter Dashboard →')).toBeInTheDocument()
  })

  it('swallows finalize 410 (already-finalized) during Open Dashboard flow', async () => {
    mockFetch({ shellOk: true, daemonReady: true, daemonRedirect: 'http://127.0.0.1:8403/krouter/', finalizeOk: false })
    render(<ShellStep onNext={() => {}} {...FAST} />)
    fireEvent.click(screen.getByText('Apply'))
    await waitFor(() => screen.getByText('Open KRouter Dashboard →'))
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })
})
