import React from 'react'

const ITEMS = [
  { icon: '📁', text: 'Daemon binary → ~/.local/bin/krouter' },
  { icon: '⚙️', text: 'Service registration (LaunchAgent / systemd --user) — no admin password required' },
  { icon: '🐚', text: 'Shell integration → ~/.zshrc (or equivalent)' },
]

export default function Step2Permissions({ onNext, onBack }) {
  return (
    <div className="space-y-6">
      <h2 className="text-xl font-semibold text-gray-800">What will be installed</h2>
      <p className="text-sm text-gray-500">No admin password required. Everything installs in your home directory.</p>
      <ul className="space-y-3">
        {ITEMS.map((item) => (
          <li key={item.text} className="flex items-start gap-3 rounded-lg border border-gray-100 bg-gray-50 px-4 py-3 text-sm text-gray-700">
            <span>{item.icon}</span>
            <span>{item.text}</span>
          </li>
        ))}
      </ul>
      <div className="flex justify-between pt-2">
        <button onClick={onBack} className="text-sm text-gray-400 hover:text-gray-600">Back</button>
        <button
          onClick={onNext}
          className="rounded-lg bg-blue-600 px-6 py-2 text-sm font-medium text-white hover:bg-blue-700"
        >
          Install
        </button>
      </div>
    </div>
  )
}
