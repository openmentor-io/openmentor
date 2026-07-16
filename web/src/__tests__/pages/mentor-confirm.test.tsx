import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ConfirmMentorEmail from '@/pages/mentor/confirm'

// Keep the page test focused: layout chrome is covered elsewhere
jest.mock('@/components', () => ({
  __esModule: true,
  NavHeader: () => <div data-testid="nav-header" />,
  Footer: () => <div data-testid="footer" />,
  MetaHeader: () => null,
  Section: Object.assign(
    ({ children }: { children: React.ReactNode }) => <section>{children}</section>,
    { Title: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2> }
  ),
}))

const mockRouter = { isReady: true, query: {} as Record<string, string | string[]> }
jest.mock('next/router', () => ({
  useRouter: () => mockRouter,
}))

function mockFetchResponse(status: number, body: unknown): void {
  ;(global.fetch as jest.Mock).mockResolvedValueOnce({
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  })
}

describe('Mentor confirm page', () => {
  beforeEach(() => {
    jest.clearAllMocks()
    mockRouter.query = {}
    global.fetch = jest.fn()
  })

  it('shows the invalid state when no token is in the URL', async () => {
    render(<ConfirmMentorEmail />)

    await waitFor(() => {
      expect(screen.getByText(/doesn't work/i)).toBeInTheDocument()
    })
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('auto-confirms the token on load and shows the success state', async () => {
    mockRouter.query = { token: 'mcf_abc123' }
    mockFetchResponse(200, { success: true })

    render(<ConfirmMentorEmail />)

    await waitFor(() => {
      expect(screen.getByText(/Profile submitted for review/i)).toBeInTheDocument()
    })
    expect(global.fetch).toHaveBeenCalledWith('/api/mentor/confirm', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: 'mcf_abc123' }),
    })
  })

  it('shows the already-confirmed state', async () => {
    mockRouter.query = { token: 'mcf_abc123' }
    mockFetchResponse(200, { success: true, already: true })

    render(<ConfirmMentorEmail />)

    await waitFor(() => {
      expect(screen.getByText(/Already confirmed ✔/)).toBeInTheDocument()
    })
  })

  it('shows the invalid state for a dead link', async () => {
    mockRouter.query = { token: 'mcf_dead' }
    mockFetchResponse(400, { success: false, code: 'invalid_token' })

    render(<ConfirmMentorEmail />)

    await waitFor(() => {
      expect(screen.getByText(/doesn't work/i)).toBeInTheDocument()
    })
  })

  it('offers a resend for an expired link and shows the resent state', async () => {
    mockRouter.query = { token: 'mcf_expired' }
    mockFetchResponse(410, { success: false, code: 'token_expired' })

    render(<ConfirmMentorEmail />)

    const resendButton = await screen.findByRole('button', {
      name: /Resend confirmation email/i,
    })
    expect(screen.getByText(/link has expired/i)).toBeInTheDocument()

    mockFetchResponse(200, { success: true })
    await userEvent.click(resendButton)

    await waitFor(() => {
      expect(screen.getByText(/Fresh link sent/i)).toBeInTheDocument()
    })
    expect(global.fetch).toHaveBeenLastCalledWith('/api/mentor/confirm-resend', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: 'mcf_expired' }),
    })
  })

  it('shows the generic error state when the request fails', async () => {
    mockRouter.query = { token: 'mcf_abc123' }
    ;(global.fetch as jest.Mock).mockRejectedValueOnce(new Error('network down'))

    render(<ConfirmMentorEmail />)

    await waitFor(() => {
      expect(screen.getByText(/Something went wrong/i)).toBeInTheDocument()
    })
  })
})
