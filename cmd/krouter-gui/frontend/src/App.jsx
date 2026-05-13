import React, { useEffect, useState } from 'react'
import { useDaemon } from './hooks/useDaemon'
import StatusCard from './components/StatusCard'
import RequestStats from './components/RequestStats'
import PresetSelector from './components/PresetSelector'
import NotificationPanel from './components/NotificationPanel'
import UpdateBanner from './components/UpdateBanner'
import Wizard from './components/Wizard'

function Dashboard() {
  const {
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
  } = useDaemon()

  const [showNotifications, setShowNotifications] = useState(false)

  async function openNotifications() {
    await fetchAnnouncements()
    setShowNotifications(true)
  }

  function closeNotifications() {
    setShowNotifications(false)
  }

  async function handleRead(id) {
    await markRead(id)
  }

  async function handleDismiss(id) {
    await dismissAnnouncement(id)
  }

  return (
    <div className="min-h-screen bg-gray-50 p-6">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-gray-800">krouter</h1>
        <div className="flex items-center gap-3">
          {status && (
            <span className="text-xs text-gray-400">v{status.version}</span>
          )}
          {/* Notification bell with unread badge */}
          <button
            onClick={openNotifications}
            className="relative rounded-full p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
            aria-label={`Notifications${unreadCount > 0 ? ` (${unreadCount} unread)` : ''}`}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              className="h-5 w-5"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={1.5}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
              />
            </svg>
            {unreadCount > 0 && (
              <span className="absolute -right-0.5 -top-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-[10px] font-bold text-white">
                {unreadCount > 9 ? '9+' : unreadCount}
              </span>
            )}
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-md space-y-4">
        <UpdateBanner updateStatus={updateStatus} />
        <StatusCard status={status} error={error} />
        <RequestStats usage={usage} />
        <PresetSelector preset={preset} onChange={changePreset} />
      </div>

      {showNotifications && (
        <NotificationPanel
          announcements={announcements}
          onClose={closeNotifications}
          onRead={handleRead}
          onDismiss={handleDismiss}
        />
      )}
    </div>
  )
}

export default function App() {
  const [firstLaunch, setFirstLaunch] = useState(null)

  useEffect(() => {
    async function check() {
      if (window.go?.main?.App?.IsFirstLaunch) {
        const result = await window.go.main.App.IsFirstLaunch()
        setFirstLaunch(result)
      } else {
        setFirstLaunch(false) // dev mode: skip wizard
      }
    }
    check()
  }, [])

  if (firstLaunch === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
      </div>
    )
  }

  if (firstLaunch) {
    return <Wizard onComplete={() => setFirstLaunch(false)} />
  }

  return <Dashboard />
}
