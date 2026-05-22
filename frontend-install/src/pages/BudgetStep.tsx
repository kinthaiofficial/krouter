import { useState } from 'react'
import { api } from '../api/client'

interface Props {
  onNext: () => void
}

export default function BudgetStep({ onNext }: Props) {
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
      setError('Failed to save budget setting. Please try again.')
      setLoading(false)
    }
  }

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-xl font-bold text-gray-900 mb-1">Daily Budget</h2>
        <p className="text-sm text-gray-500 leading-relaxed">
          KRouter will block requests once your daily spending reaches this limit.
          Set to <strong>0</strong> to disable. You can change this anytime in Settings.
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
          <span className="text-gray-500 text-sm">USD / day</span>
        </div>
        <p className="text-xs text-gray-400">Default: $50 USD. All costs are displayed in USD regardless of region.</p>
      </div>

      {error && <p className="text-red-500 text-sm">{error}</p>}

      <button
        onClick={handleContinue}
        disabled={loading}
        className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-2.5 px-6 rounded-xl transition-colors disabled:opacity-50"
      >
        {loading ? 'Saving…' : 'Continue →'}
      </button>
    </div>
  )
}
