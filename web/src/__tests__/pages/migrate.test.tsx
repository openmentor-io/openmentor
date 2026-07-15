import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Migrate from '@/pages/migrate'

// Mock Turnstile component
jest.mock('@marsidev/react-turnstile', () => ({
  __esModule: true,
  Turnstile: function MockTurnstile({ onSuccess }: { onSuccess?: (token: string) => void }) {
    return (
      <button
        type="button"
        data-testid="turnstile"
        onClick={() => onSuccess?.('mock-turnstile-token')}
      >
        Complete Turnstile
      </button>
    )
  },
}))

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

describe('Migrate page', () => {
  beforeEach(() => {
    jest.clearAllMocks()
    mockRouter.query = {}
    global.fetch = jest.fn()
  })

  it('explains the problem when no slug is in the URL', () => {
    render(<Migrate />)

    expect(screen.getByText(/doesn't point at one/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Schedule migration/i })).not.toBeInTheDocument()
  })

  it('shows the migration prompt for the slug from the URL', () => {
    mockRouter.query = { slug: 'ivan-petrov-42' }
    render(<Migrate />)

    expect(screen.getByText(/getmentor\.dev\/mentor\/ivan-petrov-42/)).toBeInTheDocument()
    expect(screen.getByText(/approved but hidden/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Schedule migration/i })).toBeDisabled()
  })

  it('enables the button after captcha and schedules the migration', async () => {
    mockRouter.query = { slug: 'Ivan-Petrov-42' } // normalized to lowercase
    ;(global.fetch as jest.Mock).mockResolvedValue({
      json: async () => ({ success: true }),
    })
    render(<Migrate />)

    await userEvent.click(screen.getByTestId('turnstile'))
    const button = screen.getByRole('button', { name: /Schedule migration/i })
    expect(button).toBeEnabled()

    await userEvent.click(button)

    await waitFor(() => {
      expect(screen.getByText(/You're on the list/i)).toBeInTheDocument()
    })
    expect(global.fetch).toHaveBeenCalledWith('/api/schedule-migration', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slug: 'ivan-petrov-42', captchaToken: 'mock-turnstile-token' }),
    })
  })

  it('shows the already-scheduled state', async () => {
    mockRouter.query = { slug: 'ivan-petrov-42' }
    ;(global.fetch as jest.Mock).mockResolvedValue({
      json: async () => ({ success: true, alreadyScheduled: true }),
    })
    render(<Migrate />)

    await userEvent.click(screen.getByTestId('turnstile'))
    await userEvent.click(screen.getByRole('button', { name: /Schedule migration/i }))

    await waitFor(() => {
      expect(screen.getByText(/Already scheduled/i)).toBeInTheDocument()
    })
  })

  it('shows an error and keeps the form usable when the request fails', async () => {
    mockRouter.query = { slug: 'ivan-petrov-42' }
    ;(global.fetch as jest.Mock).mockResolvedValue({
      json: async () => ({ success: false, error: 'Captcha verification failed' }),
    })
    render(<Migrate />)

    await userEvent.click(screen.getByTestId('turnstile'))
    await userEvent.click(screen.getByRole('button', { name: /Schedule migration/i }))

    await waitFor(() => {
      expect(screen.getByText(/Something went wrong/i)).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /Schedule migration/i })).toBeEnabled()
  })
})
