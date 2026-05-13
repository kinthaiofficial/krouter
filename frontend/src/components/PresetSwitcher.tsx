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
  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5">
      <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400 mb-3">Routing Preset</h2>
      <div className="flex gap-2">
        {presets.map((p) => (
          <button
            key={p.value}
            onClick={() => onSelect(p.value)}
            className={[
              'flex-1 rounded-lg border px-3 py-2 text-sm text-left transition-colors',
              current === p.value
                ? 'border-blue-500 bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300'
                : 'border-gray-200 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-gray-500',
            ].join(' ')}
          >
            <div className="font-medium">{p.label}</div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{p.description}</div>
          </button>
        ))}
      </div>
    </div>
  )
}
