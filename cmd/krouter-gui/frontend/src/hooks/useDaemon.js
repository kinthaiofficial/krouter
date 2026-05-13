import { useState, useEffect, useCallback } from 'react'

const MGMT_BASE = 'http://127.0.0.1:8403'
const POLL_MS = 5000

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

async function mgmtPost(token, path, body) {
  const resp = await fetch(MGMT_BASE + path, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer ' + token,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json()
}

export function useDaemon() {
  const [token, setToken] = useState('')
  const [status, setStatus] = useState(null)
  const [usage, setUsage] = useState(null)
  const [preset, setPreset] = useState('balanced')
  const [unreadCount, setUnreadCount] = useState(0)
  const [announcements, setAnnouncements] = useState([])
  const [updateStatus, setUpdateStatus] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    getToken().then(setToken)
  }, [])

  const refresh = useCallback(async () => {
    if (!token) return
    try {
      const [s, u, p, c, upd] = await Promise.all([
        mgmtFetch(token, '/internal/status'),
        mgmtFetch(token, '/internal/usage'),
        mgmtFetch(token, '/internal/preset'),
        mgmtFetch(token, '/internal/announcements/count'),
        mgmtFetch(token, '/internal/update-status'),
      ])
      setStatus(s)
      setUsage(u)
      setPreset(p.preset)
      setUnreadCount(c.unread ?? 0)
      setUpdateStatus(upd)
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }, [token])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, POLL_MS)
    return () => clearInterval(id)
  }, [refresh])

  const changePreset = useCallback(
    async (next) => {
      if (!token) return
      const body = await mgmtPost(token, '/internal/preset', { preset: next })
      setPreset(body.preset)
    },
    [token]
  )

  const fetchAnnouncements = useCallback(async () => {
    if (!token) return
    const items = await mgmtFetch(token, '/internal/announcements')
    setAnnouncements(items)
  }, [token])

  const markRead = useCallback(
    async (id) => {
      if (!token) return
      await mgmtPost(token, '/internal/announcements/read', { id })
      setAnnouncements((prev) =>
        prev.map((a) => (a.id === id ? { ...a, read_at: new Date().toISOString() } : a))
      )
      setUnreadCount((n) => Math.max(0, n - 1))
    },
    [token]
  )

  const dismissAnnouncement = useCallback(
    async (id) => {
      if (!token) return
      await mgmtPost(token, '/internal/announcements/dismiss', { id })
      setAnnouncements((prev) => prev.filter((a) => a.id !== id))
      // If the dismissed item was unread, decrement the count.
      setAnnouncements((prev) => prev) // side-effect already done above
    },
    [token]
  )

  return {
    status,
    usage,
    preset,
    unreadCount,
    announcements,
    updateStatus,
    error,
    changePreset,
    fetchAnnouncements,
    markRead,
    dismissAnnouncement,
  }
}
