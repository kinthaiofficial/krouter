import React, { useEffect } from 'react'

export default function Step5Complete({ onDone }) {
  useEffect(() => {
    async function mark() {
      if (window.go?.main?.App?.MarkSetupComplete) {
        await window.go.main.App.MarkSetupComplete()
      }
    }
    mark()
  }, [])

  return (
    <div className="space-y-6 text-center">
      <div className="text-5xl">🎉</div>
      <h2 className="text-xl font-semibold text-gray-800">krouter is ready</h2>
      <p className="text-sm text-gray-500">
        Your agents will route through krouter automatically.
        Open a new terminal for shell-based agents (Claude Code).
      </p>
      <button
        onClick={onDone}
        className="rounded-lg bg-blue-600 px-8 py-2 text-sm font-medium text-white hover:bg-blue-700"
      >
        Open Dashboard
      </button>
    </div>
  )
}
