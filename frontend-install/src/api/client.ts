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
  finalize: () => call<{ redirect_url: string }>('POST', '/api/install/finalize'),
}
