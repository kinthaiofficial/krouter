// Install wizard API client.
// The install token is passed as a URL query param (?token=...) when the
// browser is opened by krouter-installer. We read it once at startup.

export const installToken = new URLSearchParams(window.location.search).get('token') ?? ''

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const sep = path.includes('?') ? '&' : '?'
  const url = `${path}${sep}token=${encodeURIComponent(installToken)}`
  const res = await fetch(url, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error((err as { error: string }).error ?? res.statusText)
  }
  return res.json() as Promise<T>
}

export interface AgentInfo {
  name: string
  config_path?: string
  cli_path?: string
}

export interface SupportedAgent {
  agent_id: string
  display_name: string
  default_path: string
}

export interface PreviewEndpoint {
  provider: string
  endpoint_url: string
  protocol_hint?: string
  has_api_key: boolean
  has_oauth_token: boolean
}

export interface PreviewResult {
  endpoints: PreviewEndpoint[]
  error?: string
}

export interface PendingAgentSelection {
  agent_id: string
  enabled: boolean
  config_path: string
}

export const api = {
  detectAgents: () => call<AgentInfo[]>('GET', '/api/install/detect-agents'),
  copyBinary: () => call<{ ok: boolean }>('POST', '/api/install/copy-binary'),
  registerService: () => call<{ ok: boolean }>('POST', '/api/install/register-service'),
  shellIntegration: () => call<{ ok: boolean }>('POST', '/api/install/shell-integration'),
  connectAgent: (agent: string, configPath?: string) =>
    call<{ ok: boolean }>('POST', '/api/install/connect-agent', {
      agent,
      config_path: configPath ?? '',
    }),
  setBudget: (dailyLimitUSD: number) =>
    call<{ ok: boolean }>('POST', '/api/install/set-budget', { daily_limit_usd: dailyLimitUSD }),
  finalize: () => call<{ status: string }>('POST', '/api/install/finalize'),
  daemonReady: () =>
    call<{ ready: boolean; redirect_url?: string }>('GET', '/api/install/daemon-ready'),

  // Agent inheritance (spec/04) — feed the "Agent Paths" step.
  agentsSupported: () => call<SupportedAgent[]>('GET', '/api/install/agents/supported'),
  agentsPreview: (agentID: string, path: string) =>
    call<PreviewResult>('POST', '/api/install/agents/preview', { agent_id: agentID, path }),
  agentsSelect: (agents: PendingAgentSelection[]) =>
    call<{ ok: boolean; count: number }>('POST', '/api/install/agents/select', { agents }),
}
