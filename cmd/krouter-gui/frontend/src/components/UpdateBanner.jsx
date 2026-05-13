import React, { useState } from 'react'

export default function UpdateBanner({ updateStatus }) {
  const [applying, setApplying] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState(null)

  if (!updateStatus?.latest) return null

  const { latest, is_critical: isCritical, release_notes_url: releaseNotesURL } = updateStatus

  async function handleApply() {
    setApplying(true)
    setError(null)
    try {
      // Trigger apply via Go binding (Wails desktop only).
      if (window.go?.main?.App?.ApplyUpdate) {
        await window.go.main.App.ApplyUpdate()
      }
    } catch (e) {
      setError(e?.message ?? String(e))
      setApplying(false)
    }
  }

  return (
    <div
      className={`mb-4 rounded-xl px-4 py-3 text-sm ${
        isCritical
          ? 'border border-red-200 bg-red-50 text-red-800'
          : 'border border-yellow-200 bg-yellow-50 text-yellow-800'
      }`}
    >
      <div className="flex items-center justify-between gap-3">
        <div>
          <span className="font-semibold">
            {isCritical ? '⚠ Critical update: ' : 'Update available: '}
          </span>
          v{latest} is available.
          {releaseNotesURL && (
            <>
              {' '}
              <a
                href={releaseNotesURL}
                target="_blank"
                rel="noreferrer"
                className="underline hover:opacity-80"
              >
                Release notes
              </a>
            </>
          )}
        </div>
        <button
          onClick={handleApply}
          disabled={applying}
          className={`flex-shrink-0 rounded-lg px-3 py-1.5 text-xs font-semibold transition-opacity ${
            isCritical
              ? 'bg-red-600 text-white hover:bg-red-700 disabled:opacity-50'
              : 'bg-yellow-600 text-white hover:bg-yellow-700 disabled:opacity-50'
          }`}
        >
          {applying ? `Updating… ${progress > 0 ? `${progress}%` : ''}` : 'Update now'}
        </button>
      </div>
      {error && (
        <p className="mt-1 text-xs text-red-600">Update failed: {error}</p>
      )}
      {applying && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-yellow-200">
          <div
            className="h-full bg-yellow-600 transition-all duration-300"
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </div>
  )
}
