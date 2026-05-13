import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './helpers'
import Settings from '../pages/Settings'

const mockSettings = {
  preset: 'balanced' as const,
  language: 'en',
  notification_categories: { quota_warning: true, announcement_new: true, upgrade_available: false },
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

describe('Settings page', () => {
  it('renders preset buttons', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => {
      expect(screen.getByText('saver')).toBeInTheDocument()
      expect(screen.getByText('balanced')).toBeInTheDocument()
      expect(screen.getByText('quality')).toBeInTheDocument()
    })
  })

  it('highlights active preset', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText('balanced'))
    const balanced = screen.getByRole('button', { name: 'balanced' })
    expect(balanced.className).toContain('blue')
  })

  it('notification category toggles are rendered', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => {
      expect(screen.getByText('Quota Warnings')).toBeInTheDocument()
      expect(screen.getByText('New Announcements')).toBeInTheDocument()
      expect(screen.getByText('Updates Available')).toBeInTheDocument()
    })
  })

  it('upgrade_available is unchecked per mock settings', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText('Updates Available'))
    const checkboxes = screen.getAllByRole('checkbox')
    // 3rd checkbox is upgrade_available = false
    expect(checkboxes[2]).not.toBeChecked()
  })

  it('budget warning inputs are present', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => {
      expect(screen.getByText('Daily limit ($)')).toBeInTheDocument()
      expect(screen.getByText('Weekly limit ($)')).toBeInTheDocument()
    })
  })

  it('clicking different preset calls PATCH', async () => {
    renderWithProviders(<Settings />)
    await waitFor(() => screen.getByText('saver'))
    fireEvent.click(screen.getByRole('button', { name: 'saver' }))
    await waitFor(() => {
      const calls = (fetch as ReturnType<typeof vi.fn>).mock.calls
      const patch = calls.find(([, opts]) => opts?.method === 'PATCH')
      expect(patch).toBeTruthy()
    })
  })
})
