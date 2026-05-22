import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { render } from '@testing-library/react'
import AgentPathsStep from '../pages/AgentPathsStep'

type Handler = (body: unknown) => unknown
const handlers = new Map<string, Handler>()
const calls: Array<{ method: string; path: string; body: unknown }> = []

beforeEach(() => {
  handlers.clear()
  calls.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const path = url.split('?')[0]
    const body = init?.body ? JSON.parse(init.body as string) : undefined
    calls.push({ method: init?.method ?? 'GET', path, body })
    const handler = handlers.get(path)
    const responseBody = handler ? handler(body) : []
    return {
      ok: true,
      status: 200,
      json: async () => responseBody,
    } as Response
  }))
})

const supportedFixture = [
  { agent_id: 'openclaw', display_name: 'OpenClaw', default_path: '/u/.openclaw/openclaw.json' },
  { agent_id: 'claude-code', display_name: 'Claude Code', default_path: '/u/.zshrc' },
]

describe('<AgentPathsStep>', () => {
  it('renders one row per supported agent with its default path', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)

    render(<AgentPathsStep onNext={() => {}} />)
    await waitFor(() => {
      expect(screen.getByText('OpenClaw')).toBeInTheDocument()
      expect(screen.getByText('Claude Code')).toBeInTheDocument()
    })

    // Path inputs are pre-populated with the Scanner default.
    const paths = screen.getAllByRole('textbox') as HTMLInputElement[]
    expect(paths[0].value).toBe('/u/.openclaw/openclaw.json')
    expect(paths[1].value).toBe('/u/.zshrc')
  })

  it('disables Continue until at least one agent is checked', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)

    render(<AgentPathsStep onNext={() => {}} />)
    await waitFor(() => screen.getByText('OpenClaw'))

    const continueBtn = screen.getByRole('button', { name: /Select at least one agent/i })
    expect(continueBtn).toBeDisabled()

    // Tick the OpenClaw checkbox; button copy + state must change.
    const checks = screen.getAllByRole('checkbox') as HTMLInputElement[]
    fireEvent.click(checks[0])
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Continue with 1 agent/i })).not.toBeDisabled()
    })
  })

  it('runs preview and shows the vendor count', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)
    handlers.set('/api/install/agents/preview', () => ({
      endpoints: [
        { provider: 'anthropic', endpoint_url: 'u', has_api_key: true, has_oauth_token: false },
        { provider: 'minimax-portal', endpoint_url: 'u', has_api_key: false, has_oauth_token: true },
      ],
    }))

    render(<AgentPathsStep onNext={() => {}} />)
    await waitFor(() => screen.getByText('OpenClaw'))

    const previewBtns = screen.getAllByRole('button', { name: /Preview/i })
    fireEvent.click(previewBtns[0])

    await waitFor(() => {
      expect(screen.getByText(/2 vendors found: anthropic, minimax-portal/)).toBeInTheDocument()
    })
  })

  it('surfaces a preview error inline', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)
    handlers.set('/api/install/agents/preview', () => ({
      endpoints: [], error: 'config not found at /u/.openclaw/openclaw.json',
    }))

    render(<AgentPathsStep onNext={() => {}} />)
    await waitFor(() => screen.getByText('OpenClaw'))

    fireEvent.click(screen.getAllByRole('button', { name: /Preview/i })[0])
    await waitFor(() => {
      expect(screen.getByText(/config not found/)).toBeInTheDocument()
    })
  })

  it('POSTs every row to /select when the user clicks Continue', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)
    handlers.set('/api/install/agents/select', () => ({ ok: true, count: 2 }))

    const onNext = vi.fn()
    render(<AgentPathsStep onNext={onNext} />)
    await waitFor(() => screen.getByText('OpenClaw'))

    // Enable OpenClaw, leave claude-code unchecked.
    const checks = screen.getAllByRole('checkbox') as HTMLInputElement[]
    fireEvent.click(checks[0])

    fireEvent.click(screen.getByRole('button', { name: /Continue with 1 agent/i }))
    await waitFor(() => expect(onNext).toHaveBeenCalled())

    const selectCall = calls.find((c) => c.path === '/api/install/agents/select')
    expect(selectCall).toBeTruthy()
    const body = selectCall!.body as { agents: { agent_id: string; enabled: boolean }[] }
    expect(body.agents).toHaveLength(2)
    expect(body.agents.find((a) => a.agent_id === 'openclaw')?.enabled).toBe(true)
    expect(body.agents.find((a) => a.agent_id === 'claude-code')?.enabled).toBe(false)
  })

  it('editing the path clears stale preview state', async () => {
    handlers.set('/api/install/agents/supported', () => supportedFixture)
    handlers.set('/api/install/agents/preview', () => ({
      endpoints: [{ provider: 'anthropic', endpoint_url: 'u', has_api_key: true, has_oauth_token: false }],
    }))

    render(<AgentPathsStep onNext={() => {}} />)
    await waitFor(() => screen.getByText('OpenClaw'))

    // First preview → shows 1 vendor
    fireEvent.click(screen.getAllByRole('button', { name: /Preview/i })[0])
    await waitFor(() => {
      expect(screen.getByText(/1 vendor found: anthropic/)).toBeInTheDocument()
    })

    // Edit path → preview state should be cleared
    const pathInput = (screen.getAllByRole('textbox') as HTMLInputElement[])[0]
    fireEvent.change(pathInput, { target: { value: '/new/path.json' } })

    await waitFor(() => {
      expect(screen.queryByText(/1 vendor found/)).not.toBeInTheDocument()
    })
  })
})
