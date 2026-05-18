import type { QuotaItem } from '../api/client'

interface Props {
  quota: QuotaItem
}

const windowLabels: Record<string, string> = {
  '5h': '5-Hour Window',
  weekly: 'Weekly Window',
  opus: 'Opus Tokens',
}

// Rough token limits per window type (Anthropic Pro plan).
const windowLimits: Record<string, number> = {
  '5h': 50_000,
  weekly: 1_000_000,
  opus: 100_000,
}

export default function QuotaBar({ quota }: Props) {
  const limit = windowLimits[quota.window] ?? 1
  const pct = Math.min(100, (quota.tokens_used / limit) * 100)
  const color = pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-yellow-500' : 'bg-brand'

  return (
    <div>
      <div className="flex justify-between text-xs mb-1">
        <span className="text-gray-600">
          {windowLabels[quota.window] ?? quota.window}
        </span>
        <span className="text-gray-500 font-mono">
          {quota.tokens_used.toLocaleString()} / {limit.toLocaleString()}
        </span>
      </div>
      <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}
