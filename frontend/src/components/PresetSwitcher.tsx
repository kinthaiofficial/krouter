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
    { value: 'passthrough', label: t('preset.passthrough'), description: t('preset.passthrough_desc') },
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
    <div className="bg-card rounded-xl border border-line h-full">
      <div className="px-4 py-3 border-b border-line">
        <h2 className="text-xs font-bold uppercase tracking-wider text-muted">{t('preset.title')}</h2>
      </div>
      <div className="p-4 flex flex-col gap-1.5">
        {presets.map((p) => {
          const active = display === p.value
          return (
            <button
              key={p.value}
              onClick={() => handleClick(p.value)}
              className={[
                'flex items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors',
                active
                  ? 'border-brand bg-brand-soft'
                  : 'border-line hover:border-line-strong hover:bg-gray-50',
              ].join(' ')}
            >
              <span
                className={[
                  'w-[15px] h-[15px] rounded-full shrink-0 border-[1.5px]',
                  active
                    ? 'border-brand shadow-[inset_0_0_0_3px_var(--color-brand)]'
                    : 'border-line-strong',
                ].join(' ')}
              />
              <span>
                <span className={['block text-sm font-semibold', active ? 'text-brand-ink' : 'text-ink'].join(' ')}>
                  {p.label}
                </span>
                <span className="block text-xs text-muted mt-0.5">{p.description}</span>
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
