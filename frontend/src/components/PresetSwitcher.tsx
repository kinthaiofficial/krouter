import { useState, useEffect } from 'react'
import type { Preset } from '../api/client'

interface Props {
  current: Preset
  onSelect: (preset: Preset) => void
}

const presets: { value: Preset; label: string; description: string }[] = [
  { value: 'saver', label: 'Saver', description: 'Cheapest available models' },
  { value: 'balanced', label: 'Balanced', description: 'Quality vs cost tradeoff' },
  { value: 'quality', label: 'Quality', description: 'Best available models' },
]

export default function PresetSwitcher({ current, onSelect }: Props) {
  const [optimistic, setOptimistic] = useState<Preset | null>(null)
  const display = optimistic ?? current

  useEffect(() => {
    setOptimistic(null)
  }, [current])

  function handleClick(p: Preset) {
    if (p === display) return
    setOptimistic(p)
    onSelect(p)
  }

  return (
    <div className="bg-white rounded-xl border border-border p-5">
      <h2 className="text-sm font-medium text-gray-500 mb-3">Routing Preset</h2>
      <div className="flex gap-2">
        {presets.map((p) => (
          <button
            key={p.value}
            onClick={() => handleClick(p.value)}
            className={[
              'flex-1 rounded-lg border px-3 py-2 text-sm text-left transition-colors',
              display === p.value
                ? 'border-brand bg-brand-light text-brand font-semibold'
                : 'border-border text-gray-700 hover:border-brand/50',
            ].join(' ')}
          >
            <div className="font-medium">{p.label}</div>
            <div className="text-xs text-gray-500 mt-0.5">{p.description}</div>
          </button>
        ))}
      </div>
    </div>
  )
}
