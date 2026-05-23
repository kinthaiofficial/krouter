import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { Preset } from '../api/client'

interface Props {
  current: Preset
  onSelect: (preset: Preset) => void
}

export default function PresetSwitcher({ current, onSelect }: Props) {
  const { t } = useTranslation()
  const [optimistic, setOptimistic] = useState<Preset | null>(null)
  const display = optimistic ?? current

  const presets: { value: Preset; label: string; description: string }[] = [
    { value: 'saver', label: t('preset.saver'), description: t('preset.saver_desc') },
    { value: 'balanced', label: t('preset.balanced'), description: t('preset.balanced_desc') },
    { value: 'quality', label: t('preset.quality'), description: t('preset.quality_desc') },
  ]

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
      <h2 className="text-sm font-medium text-gray-500 mb-3">{t('preset.title')}</h2>
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
