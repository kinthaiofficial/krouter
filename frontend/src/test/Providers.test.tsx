import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import Providers from '../pages/Providers'

const mockProviders = [
  { name: 'anthropic', protocol: 'anthropic', available: true, consecutive_failures: 0, success_rate: 1.0 },
  { name: 'openai', protocol: 'openai', available: false, consecutive_failures: 3, success_rate: 0.7 },
]

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true, status: 200, json: () => Promise.resolve(mockProviders),
  }))
})

describe('Providers page', () => {
  it('shows configured providers', async () => {
    renderWithProviders(<Providers />)
    await waitFor(() => {
      expect(screen.getByText('Anthropic')).toBeInTheDocument()
      expect(screen.getByText('OpenAI')).toBeInTheDocument()
    })
  })

  it('shows not-configured providers with setup hint', async () => {
    renderWithProviders(<Providers />)
    await waitFor(() => {
      // DeepSeek, Moonshot, etc. should appear as missing
      expect(screen.getByText('DeepSeek')).toBeInTheDocument()
      expect(screen.getByText('Set DEEPSEEK_API_KEY to enable')).toBeInTheDocument()
    })
  })

  it('shows failure count for unhealthy provider', async () => {
    renderWithProviders(<Providers />)
    await waitFor(() => {
      // consecutive_failures: 3 for openai
      expect(screen.getByText('3')).toBeInTheDocument()
    })
  })
})
