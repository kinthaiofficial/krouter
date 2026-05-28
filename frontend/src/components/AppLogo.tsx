import { useState } from 'react'

// Brand colors for letter fallback when no logo file exists
const APP_COLORS: Record<string, string> = {
  'openclaw':  '#0fa46a',
  'hermes':    '#8b5cf6',
  'opencode':  '#0ea5e9',
  'pi':        '#f59e0b',
}

interface Props {
  appId: string
  size?: number         // px, default 32
  className?: string
  connected?: boolean   // drives fallback ring color
}

export default function AppLogo({ appId, size = 32, className = '', connected = false }: Props) {
  const [failed, setFailed] = useState(false)

  const style = { width: size, height: size }
  const src = `/krouter/logos/apps/${appId}.svg`

  if (!failed) {
    return (
      <img
        src={src}
        alt={appId}
        style={style}
        className={`shrink-0 rounded-md object-contain bg-white p-0.5 border border-gray-100 ${className}`}
        onError={() => setFailed(true)}
      />
    )
  }

  // Fallback: letter circle
  const bg = APP_COLORS[appId] ?? (connected ? 'var(--color-brand)' : '#9ca3af')
  const letters = appId.slice(0, 2).toUpperCase()
  return (
    <span
      style={{ ...style, backgroundColor: bg, fontSize: size * 0.34 }}
      className={`shrink-0 inline-flex items-center justify-center rounded-full text-white font-bold tracking-tight ${className}`}
    >
      {letters}
    </span>
  )
}
