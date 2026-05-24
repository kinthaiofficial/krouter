import type { ReactNode } from 'react'

// ── B3 "Pro Control Panel" shared primitives ───────────────────────────
// Reused across every page so the whole app reads as one cohesive surface:
// bordered cards with an uppercase head, legible (non-gray) text, monospace
// tabular numbers for any figure.

export function PageHeader({ title, subtitle, right }: {
  title: string; subtitle?: string; right?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 mb-5 flex-wrap">
      <div>
        <h1 className="text-lg font-bold tracking-tight text-ink">{title}</h1>
        {subtitle && <p className="text-sm text-muted mt-0.5">{subtitle}</p>}
      </div>
      {right && <div className="flex items-center gap-2 shrink-0">{right}</div>}
    </div>
  )
}

export function Panel({ title, right, children, className, flush }: {
  title?: string; right?: ReactNode; children: ReactNode; className?: string; flush?: boolean
}) {
  return (
    <section className={['bg-card border border-line rounded-xl', className ?? ''].join(' ')}>
      {title && (
        <div className="flex items-center justify-between px-4 py-3 border-b border-line">
          <h2 className="text-xs font-bold uppercase tracking-wider text-muted">{title}</h2>
          {right}
        </div>
      )}
      <div className={flush ? '' : 'p-4'}>{children}</div>
    </section>
  )
}

export function Badge({ children, tone = 'brand' }: {
  children: ReactNode; tone?: 'brand' | 'subtle' | 'amber' | 'red'
}) {
  const tones: Record<string, string> = {
    brand: 'text-brand-ink bg-brand-soft',
    subtle: 'text-muted bg-gray-100',
    amber: 'text-amber-700 bg-amber-50',
    red: 'text-red-700 bg-red-50',
  }
  return (
    <span className={['text-xs font-semibold px-2.5 py-1 rounded-md', tones[tone]].join(' ')}>
      {children}
    </span>
  )
}

export function StatusDot({ code }: { code: number }) {
  const color = code >= 500 ? 'bg-red-500' : code >= 400 ? 'bg-amber-500' : 'bg-brand'
  return (
    <span className="inline-flex items-center gap-1.5 font-mono tabular-nums text-faint">
      <span className={`w-[7px] h-[7px] rounded-full ${color}`} />
      {code}
    </span>
  )
}
