import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import DeleteProfileCard from '@/components/mentor-admin/DeleteProfileCard'

jest.mock('@/lib/posthog', () => ({
  captureException: jest.fn(),
}))

const USERNAME = 'anna-petrova-42'

function mockFetchResponse(ok: boolean, body: unknown): jest.Mock {
  const mock = jest.fn().mockResolvedValue({ ok, json: async () => body })
  global.fetch = mock as unknown as typeof fetch
  return mock
}

function openDialogAndConfirm(typed = USERNAME): void {
  fireEvent.click(screen.getByRole('button', { name: 'Delete profile' }))
  fireEvent.change(screen.getByLabelText(/to confirm/i), { target: { value: typed } })
  fireEvent.click(screen.getByRole('button', { name: /delete profile permanently/i }))
}

describe('DeleteProfileCard', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('points an approved mentor at the visibility toggle as the reversible alternative', () => {
    render(<DeleteProfileCard username={USERNAME} canHideInstead onDeleted={jest.fn()} />)

    expect(screen.getByText(/only want to step away for a while/i)).toBeInTheDocument()
  })

  // A draft/pending profile has no visibility toggle on the page, so offering
  // it as the alternative would send the mentor looking for a control that
  // isn't there.
  it('omits that hint when there is no visibility toggle to point at', () => {
    render(<DeleteProfileCard username={USERNAME} onDeleted={jest.fn()} />)

    expect(screen.queryByText(/only want to step away for a while/i)).not.toBeInTheDocument()
  })

  it('opens the confirm dialog rather than deleting on the first click', () => {
    const fetchMock = mockFetchResponse(true, { success: true })
    render(<DeleteProfileCard username={USERNAME} onDeleted={jest.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete profile' }))

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('posts the typed username and reports the deletion', async () => {
    const fetchMock = mockFetchResponse(true, { success: true })
    const onDeleted = jest.fn()
    render(<DeleteProfileCard username={USERNAME} onDeleted={onDeleted} />)

    openDialogAndConfirm()

    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/mentor/profile/delete',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        body: JSON.stringify({ username: USERNAME }),
      })
    )
  })

  // The server verifies the confirmation, so the request must carry what the
  // mentor TYPED — sending the canonical username prop would reduce the
  // server-side check to confirming a value the client already knew.
  it('sends the typed value verbatim, not the username prop', async () => {
    const fetchMock = mockFetchResponse(true, { success: true })
    render(<DeleteProfileCard username={USERNAME} onDeleted={jest.fn()} />)

    openDialogAndConfirm('  ANNA-Petrova-42  ')

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/mentor/profile/delete',
        expect.objectContaining({
          body: JSON.stringify({ username: '  ANNA-Petrova-42  ' }),
        })
      )
    )
  })

  it('surfaces the server message and does not report a deletion that failed', async () => {
    mockFetchResponse(false, { error: 'The username you entered does not match your profile' })
    const onDeleted = jest.fn()
    render(<DeleteProfileCard username={USERNAME} onDeleted={onDeleted} />)

    openDialogAndConfirm()

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'The username you entered does not match your profile'
      )
    )
    expect(onDeleted).not.toHaveBeenCalled()
  })
})
