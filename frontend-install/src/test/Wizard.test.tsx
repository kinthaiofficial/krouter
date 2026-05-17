import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { render } from '@testing-library/react'
import App from '../App'
import WelcomeStep from '../pages/WelcomeStep'
import DetectStep from '../pages/DetectStep'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: () => Promise.resolve([]),
  }))
})

describe('WelcomeStep', () => {
  it('renders title and get started button', () => {
    render(<WelcomeStep onNext={() => {}} />)
    expect(screen.getByText('Stop overpaying for AI tokens.')).toBeInTheDocument()
    expect(screen.getByText('Get Started')).toBeInTheDocument()
  })

  it('calls onNext when Get Started is clicked', () => {
    const onNext = vi.fn()
    render(<WelcomeStep onNext={onNext} />)
    fireEvent.click(screen.getByText('Get Started'))
    expect(onNext).toHaveBeenCalledOnce()
  })
})

describe('DetectStep', () => {
  it('shows no agents message when detect returns empty', async () => {
    render(<DetectStep onNext={() => {}} />)
    await waitFor(() => {
      expect(screen.getByText(/No compatible agents found/)).toBeInTheDocument()
    })
  })

  it('lists detected agents', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve([
        { name: 'openclaw', config_path: '/home/user/.openclaw/openclaw.json' },
        { name: 'claude-code', cli_path: '/usr/bin/claude' },
      ]),
    }))
    render(<DetectStep onNext={() => {}} />)
    await waitFor(() => {
      expect(screen.getByText('OpenClaw')).toBeInTheDocument()
      expect(screen.getByText('Claude Code')).toBeInTheDocument()
    })
  })

  it('skip button calls onNext without API call', async () => {
    const onNext = vi.fn()
    render(<DetectStep onNext={onNext} />)
    await waitFor(() => screen.getByText(/No compatible agents found/))
    fireEvent.click(screen.getByText('Skip'))
    expect(onNext).toHaveBeenCalledOnce()
  })
})

describe('App wizard flow', () => {
  it('starts on Welcome step', () => {
    render(<App />)
    expect(screen.getByText('Stop overpaying for AI tokens.')).toBeInTheDocument()
  })

  it('advances to Detect step after Get Started', () => {
    render(<App />)
    fireEvent.click(screen.getByText('Get Started'))
    expect(screen.getByText('Detected AI agents')).toBeInTheDocument()
  })
})
