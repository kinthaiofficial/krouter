import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'

interface Props {
  onNext: () => void
}

export default function BudgetStep({ onNext }: Props) {
  const { t } = useTranslation()
  const [limit, setLimit] = useState('50')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function handleContinue() {
    setLoading(true)
    setError('')
    try {
      const v = parseFloat(limit)
      await api.setBudget(isNaN(v) || v < 0 ? 50 : v)
      onNext()
    } catch {
      setError(t('budget.failed'))
      setLoading(false)
    }
  }

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-xl font-bold text-gray-900 mb-1">{t('budget.title')}</h2>
        <p className="text-sm text-gray-500 leading-relaxed">
          {t('budget.detail')}
        </p>
      </div>

      <div className="bg-gray-50 rounded-xl p-4 space-y-3">
        <div className="flex items-center gap-3">
          <span className="text-gray-500 text-sm font-medium">$</span>
          <input
            type="number"
            min={0}
            step={1}
            value={limit}
            onChange={(e) => setLimit(e.target.value)}
            className="w-32 border border-gray-200 rounded-lg px-3 py-2 text-sm bg-white focus:outline-none focus:border-brand"
          />
          <span className="text-gray-500 text-sm">{t('budget.unit')}</span>
        </div>
        <p className="text-xs text-gray-400">{t('budget.note')}</p>
      </div>

      {error && <p className="text-red-500 text-sm">{error}</p>}

      <button
        onClick={handleContinue}
        disabled={loading}
        className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-2.5 px-6 rounded-xl transition-colors disabled:opacity-50"
      >
        {loading ? t('budget.saving') : t('budget.continue')}
      </button>
    </div>
  )
}
