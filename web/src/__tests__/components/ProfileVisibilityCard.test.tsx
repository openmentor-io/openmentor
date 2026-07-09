import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import ProfileVisibilityCard from '@/components/mentor-admin/ProfileVisibilityCard'

jest.mock('@/lib/posthog', () => ({
  captureException: jest.fn(),
}))

const HELPER_TEXT =
  "Your profile is hidden from the catalog. Mentees can't send you new requests, but you can still manage existing ones."

function mockFetchResponse(ok: boolean, body: unknown): jest.Mock {
  const mock = jest.fn().mockResolvedValue({
    ok,
    json: async () => body,
  })
  global.fetch = mock as unknown as typeof fetch
  return mock
}

describe('ProfileVisibilityCard', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('renders the toggle on for an active profile without helper text', () => {
    render(<ProfileVisibilityCard initialStatus="active" />)

    expect(screen.getByText('Profile visibility')).toBeInTheDocument()
    expect(screen.getByText('Show my profile in the mentor catalog')).toBeInTheDocument()
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText(HELPER_TEXT)).not.toBeInTheDocument()
  })

  it('renders the toggle off for an inactive profile with helper text', () => {
    render(<ProfileVisibilityCard initialStatus="inactive" />)

    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(HELPER_TEXT)).toBeInTheDocument()
  })

  it('calls the status API and reports success when toggled off', async () => {
    const fetchMock = mockFetchResponse(true, { success: true, status: 'inactive' })
    const onSuccess = jest.fn()

    render(<ProfileVisibilityCard initialStatus="active" onSuccess={onSuccess} />)

    fireEvent.click(screen.getByRole('switch'))

    // Optimistic update: helper text appears immediately
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(HELPER_TEXT)).toBeInTheDocument()

    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith('inactive'))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/mentor/profile/status',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        body: JSON.stringify({ status: 'inactive' }),
      })
    )
  })

  it('calls the status API with active when toggled back on', async () => {
    const fetchMock = mockFetchResponse(true, { success: true, status: 'active' })
    const onSuccess = jest.fn()

    render(<ProfileVisibilityCard initialStatus="inactive" onSuccess={onSuccess} />)

    fireEvent.click(screen.getByRole('switch'))

    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith('active'))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/mentor/profile/status',
      expect.objectContaining({
        body: JSON.stringify({ status: 'active' }),
      })
    )
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText(HELPER_TEXT)).not.toBeInTheDocument()
  })

  it('rolls back the optimistic update and shows an error when the API fails', async () => {
    mockFetchResponse(false, { error: 'Failed to update profile status' })
    const onSuccess = jest.fn()

    render(<ProfileVisibilityCard initialStatus="active" onSuccess={onSuccess} />)

    fireEvent.click(screen.getByRole('switch'))

    await waitFor(() =>
      expect(
        screen.getByText('Failed to update profile visibility. Please try again.')
      ).toBeInTheDocument()
    )

    // Rolled back to active
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText(HELPER_TEXT)).not.toBeInTheDocument()
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it('rolls back when the network request throws', async () => {
    const fetchMock = jest.fn().mockRejectedValue(new Error('network down'))
    global.fetch = fetchMock as unknown as typeof fetch

    render(<ProfileVisibilityCard initialStatus="inactive" />)

    fireEvent.click(screen.getByRole('switch'))

    await waitFor(() =>
      expect(
        screen.getByText('Failed to update profile visibility. Please try again.')
      ).toBeInTheDocument()
    )

    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(HELPER_TEXT)).toBeInTheDocument()
  })
})
