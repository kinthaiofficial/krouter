import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import '../i18n'
import Settings from '../pages/Settings'

const mockSettings = {
  preset: 'balanced' as const,
  language: 'en',
  notification_categories: {},
  budget_warnings: { daily: 5.0, weekly: 20.0 },
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((_url: string, opts?: RequestInit) => {
    if (opts?.method === 'PATCH') {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(mockSettings) })
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(mockSettings) })
  }))
})

describe('Settings page (cleaned up)', () => {
  it('renders the language selector', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => {
      expect(screen.getByText(/Language|语言/i)).toBeInTheDocument()
    })
  })

  it('renders the Data Management section', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => {
      expect(screen.getByText(/Data Management|数据管理/)).toBeInTheDocument()
    })
  })

  it('no longer renders the Routing Preset section', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText(/Language|语言/))
    // The saver/balanced/quality preset buttons should be absent.
    expect(screen.queryByRole('button', { name: /^saver$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^quality$/i })).not.toBeInTheDocument()
  })

  it('no longer renders Desktop Notifications checkboxes', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText(/Language|语言/))
    // The only checkboxes on the page should be zero now (data mgmt has none).
    expect(screen.queryAllByRole('checkbox').length).toBe(0)
  })

  it('no longer renders the Pricing Data section', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText(/Language|语言/))
    expect(screen.queryByText(/Pricing Data|定价数据/)).not.toBeInTheDocument()
  })

  it('no longer renders the in-line Budget Limits section', async () => {
    // Budget config moved to its own top-level page (#4).
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText(/Language|语言/))
    expect(screen.queryByText(/Budget Limits|预算限额/)).not.toBeInTheDocument()
  })
})
