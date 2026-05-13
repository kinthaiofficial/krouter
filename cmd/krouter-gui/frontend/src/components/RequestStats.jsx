import React from 'react'

export default function RequestStats({ usage }) {
  const requests = usage?.requests_today ?? '—'
  const cost = usage?.cost_today_usd != null
    ? `$${Number(usage.cost_today_usd).toFixed(4)}`
    : '—'
  const savings = usage?.savings_today_usd != null && usage.savings_today_usd > 0
    ? `$${Number(usage.savings_today_usd).toFixed(4)}`
    : null

  return (
    <div className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">
        Today
      </h2>
      <div className="flex items-center gap-6">
        <div>
          <p className="text-2xl font-bold text-gray-800">{requests}</p>
          <p className="text-xs text-gray-400">requests</p>
        </div>
        <div>
          <p className="text-2xl font-bold text-gray-800">{cost}</p>
          <p className="text-xs text-gray-400">cost</p>
        </div>
        {savings && (
          <div>
            <p className="text-2xl font-bold text-green-600">{savings}</p>
            <p className="text-xs text-gray-400">saved</p>
          </div>
        )}
      </div>
    </div>
  )
}
