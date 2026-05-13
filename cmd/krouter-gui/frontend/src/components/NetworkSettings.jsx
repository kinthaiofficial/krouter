import React, { useEffect, useState, useCallback } from 'react'

const MGMT_BASE = 'http://127.0.0.1:8403'

async function getToken() {
  if (window.go?.main?.App?.GetToken) {
    return window.go.main.App.GetToken()
  }
  return ''
}

async function mgmtFetch(token, path) {
  const resp = await fetch(MGMT_BASE + path, {
    headers: { Authorization: 'Bearer ' + token },
  })
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json()
}

async function mgmtPost(token, path) {
  const resp = await fetch(MGMT_BASE + path, {
    method: 'POST',
    headers: { Authorization: 'Bearer ' + token },
  })
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json()
}

async function mgmtDelete(token, path) {
  const resp = await fetch(MGMT_BASE + path, {
    method: 'DELETE',
    headers: { Authorization: 'Bearer ' + token },
  })
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json()
}

export default function NetworkSettings() {
  const [token, setToken] = useState('')
  const [status, setStatus] = useState(null)
  const [devices, setDevices] = useState([])
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    getToken().then(setToken)
  }, [])

  const refresh = useCallback(async () => {
    if (!token) return
    try {
      const [st, devs] = await Promise.all([
        mgmtFetch(token, '/internal/remote/status'),
        mgmtFetch(token, '/internal/devices'),
      ])
      setStatus(st)
      setDevices(devs ?? [])
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }, [token])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, 5000)
    return () => clearInterval(id)
  }, [refresh])

  async function handleEnable() {
    setLoading(true)
    try {
      await mgmtPost(token, '/internal/remote/enable')
      await refresh()
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function handleDisable() {
    setLoading(true)
    try {
      await mgmtPost(token, '/internal/remote/disable')
      await refresh()
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function handleRevoke(deviceId) {
    try {
      await mgmtDelete(token, `/internal/devices/${deviceId}`)
      await refresh()
    } catch (e) {
      setError(e.message)
    }
  }

  return (
    <div className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-500">
        LAN Remote Access
      </h2>

      {error && (
        <p className="mb-2 text-xs text-red-500">{error}</p>
      )}

      {status && (
        <div className="mb-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-800">
                {status.enabled ? 'Enabled' : 'Disabled'}
              </p>
              {status.enabled && status.token && (
                <p className="mt-0.5 font-mono text-xs text-gray-500">{status.token}</p>
              )}
              {status.enabled && status.expires_in > 0 && (
                <p className="text-xs text-gray-400">
                  Pairing window: {status.expires_in}s remaining
                </p>
              )}
            </div>
            <button
              onClick={status.enabled ? handleDisable : handleEnable}
              disabled={loading}
              className={`rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
                status.enabled
                  ? 'bg-gray-100 text-gray-700 hover:bg-gray-200'
                  : 'bg-blue-600 text-white hover:bg-blue-700'
              }`}
            >
              {loading ? '…' : status.enabled ? 'Disable' : 'Enable'}
            </button>
          </div>
        </div>
      )}

      {devices.length > 0 && (
        <div>
          <p className="mb-1 text-xs font-semibold text-gray-400">Paired devices</p>
          <ul className="divide-y divide-gray-50">
            {devices.map((d) => (
              <li key={d.id} className="flex items-center justify-between py-1.5">
                <div>
                  <p className="text-sm text-gray-700">{d.name || 'Unnamed device'}</p>
                  <p className="text-xs text-gray-400">{d.ip_address}</p>
                </div>
                <button
                  onClick={() => handleRevoke(d.id)}
                  className="rounded p-1 text-xs text-red-400 hover:bg-red-50 hover:text-red-600"
                >
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
