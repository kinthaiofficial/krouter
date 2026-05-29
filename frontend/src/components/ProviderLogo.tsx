import { useState } from 'react'

// Brand colors for letter fallback
const PROVIDER_COLORS: Record<string, string> = {
  anthropic:  '#d97706',
  openai:     '#10a37f',
  deepseek:   '#5786fe',
  groq:       '#f55036',
  mistral:    '#fa520f',
  moonshot:   '#1a1a2e',
  ollama:     '#374151',
  minimax:    '#1d4ed8',
  zai:        '#6366f1',
  qwen:       '#ff6a00',
  glm:        '#1d6ae5',
  perplexity: '#1fb8cd',
  xai:        '#111827',
  fireworks:  '#7c3aed',
  together:   '#0ea5e9',
  openrouter: '#6467f2',
  gemini:     '#1a73e8',
}

interface Props {
  name: string
  size?: number      // px, default 20
  className?: string
}

export default function ProviderLogo({ name, size = 20, className = '' }: Props) {
  const [failed, setFailed] = useState(false)

  const style = { width: size, height: size }
  const src = `/krouter/logos/providers/${name}.svg`

  if (!failed) {
    return (
      <img
        src={src}
        alt={name}
        style={style}
        className={`shrink-0 object-contain ${className}`}
        onError={() => setFailed(true)}
      />
    )
  }

  // Fallback: colored letter
  const bg = PROVIDER_COLORS[name] ?? '#6b7280'
  return (
    <span
      style={{ ...style, backgroundColor: bg, fontSize: size * 0.55 }}
      className={`shrink-0 inline-flex items-center justify-center rounded text-white font-bold ${className}`}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  )
}
