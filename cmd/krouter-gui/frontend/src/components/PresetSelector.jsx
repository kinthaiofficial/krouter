import React from 'react'

const PRESETS = [
  { value: 'saver', label: 'Saver', desc: 'Cheapest model that can handle the task' },
  { value: 'balanced', label: 'Balanced', desc: 'Cost vs. quality sweet spot' },
  { value: 'quality', label: 'Quality', desc: 'Best model regardless of cost' },
]

export default function PresetSelector({ preset, onChange }) {
  return (
    <div className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-500">
        Preset
      </h2>
      <div className="flex gap-2">
        {PRESETS.map((p) => (
          <button
            key={p.value}
            onClick={() => onChange(p.value)}
            title={p.desc}
            className={`flex-1 rounded-lg border px-3 py-2 text-sm font-medium transition-colors ${
              preset === p.value
                ? 'border-blue-500 bg-blue-50 text-blue-700'
                : 'border-gray-200 text-gray-600 hover:bg-gray-50'
            }`}
          >
            {p.label}
          </button>
        ))}
      </div>
    </div>
  )
}
