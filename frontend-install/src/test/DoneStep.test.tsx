import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { render } from '@testing-library/react'
import DoneStep from '../pages/DoneStep'

// Use maxAttempts=2, pollIntervalMs=0 in all tests so the loop runs fast.
const FAST = { maxAttempts: 2, pollIntervalMs: 0 }

function mockFetch(daemonResponse: object, finalizeOk = true) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if ((url as string).includes('/daemon-ready')) {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(daemonResponse) })
    }
    if ((url as string).includes('/finalize')) {
      if (!finalizeOk) {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({ error: 'server error' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) })
  }))
}

beforeEach(() => {
  // Suppress jsdom navigation errors.
  Object.defineProperty(window, 'location', {
    value: { href: '' },
    configurable: true,
    writable: true,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DoneStep — initial render', () => {
  it('shows success message and button', () => {
    render(<DoneStep {...FAST} />)
    expect(screen.getByText('All set!')).toBeInTheDocument()
    expect(screen.getByText('Open KRouter Dashboard →')).toBeInTheDocument()
    expect(screen.queryByText(/Starting KRouter/)).not.toBeInTheDocument()
    expect(screen.queryByText(/took too long/)).not.toBeInTheDocument()
  })
})

describe('DoneStep — happy path', () => {
  it('shows spinner immediately after clicking button', async () => {
    mockFetch({ ready: false })
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    expect(await screen.findByText('Starting KRouter daemon…')).toBeInTheDocument()
    expect(screen.queryByText('Open KRouter Dashboard →')).not.toBeInTheDocument()
  })

  it('navigates when daemon is ready on first poll', async () => {
    mockFetch({ ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' })
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })

  it('navigates when daemon becomes ready on a later poll', async () => {
    let calls = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/daemon-ready')) {
        calls++
        const ready = calls >= 2
        return Promise.resolve({
          ok: true, status: 200,
          json: () => Promise.resolve(ready
            ? { ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' }
            : { ready: false }),
        })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
    }))
    render(<DoneStep maxAttempts={5} pollIntervalMs={0} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })
})

describe('DoneStep — error: daemon never starts', () => {
  it('shows error with /krouter/ URL after timeout', async () => {
    mockFetch({ ready: false })
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
    expect(screen.getByText(/8403\/krouter\//)).toBeInTheDocument()
  })

  it('button reappears after timeout so user can retry', async () => {
    mockFetch({ ready: false })
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => screen.getByText(/KRouter took too long/))
    expect(screen.getByText('Open KRouter Dashboard →')).toBeInTheDocument()
  })

  it('error clears and polling restarts on retry', async () => {
    let attempt = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/daemon-ready')) {
        attempt++
        // First two calls (first attempt): not ready.
        // Third call (retry): ready.
        const ready = attempt >= 3
        return Promise.resolve({
          ok: true, status: 200,
          json: () => Promise.resolve(ready
            ? { ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' }
            : { ready: false }),
        })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
    }))
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => screen.getByText(/KRouter took too long/))
    // Retry
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })
})

describe('DoneStep — error: network failure', () => {
  it('shows error when all polls throw network errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/finalize')) {
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
      }
      return Promise.reject(new Error('network error'))
    }))
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
    expect(screen.getByText(/8403\/krouter\//)).toBeInTheDocument()
  })

  it('shows error when daemon-ready returns non-2xx every time', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/daemon-ready')) {
        return Promise.resolve({ ok: false, status: 503, json: () => Promise.resolve({ error: 'unavailable' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ status: 'ok' }) })
    }))
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
  })
})

describe('DoneStep — finalize edge cases', () => {
  it('swallows finalize 410 (already finalized) and still polls', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if ((url as string).includes('/finalize')) {
        return Promise.resolve({ ok: false, status: 410, json: () => Promise.resolve({ error: 'already finalized' }) })
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' }) })
    }))
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })

  it('swallows finalize 500 and still polls', async () => {
    mockFetch({ ready: true, redirect_url: 'http://127.0.0.1:8403/krouter/' }, false)
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    await waitFor(() => {
      expect((window.location as { href: string }).href).toBe('http://127.0.0.1:8403/krouter/')
    })
  })

  it('uses bare /krouter/ URL when redirect_url is missing', async () => {
    // daemon-ready returns ready:true but no redirect_url — loop won't navigate
    mockFetch({ ready: true })
    render(<DoneStep {...FAST} />)
    fireEvent.click(screen.getByText('Open KRouter Dashboard →'))
    // Without redirect_url the condition `res.ready && res.redirect_url` is false,
    // so the loop exhausts and shows the timeout error with the fallback URL.
    await waitFor(() => {
      expect(screen.getByText(/KRouter took too long to start/)).toBeInTheDocument()
    })
    expect(screen.getByText(/8403\/krouter\//)).toBeInTheDocument()
  })
})
