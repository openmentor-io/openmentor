import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Migrate from '@/pages/migrate'

// Mock Turnstile component (forwardRef so the widget's reset() is callable)
const mockTurnstileReset = jest.fn()
let freshTokenSeq = 0
jest.mock('@marsidev/react-turnstile', () => {
  const react = jest.requireActual('react')
  return {
    __esModule: true,
    Turnstile: react.forwardRef(function MockTurnstile(
      { onSuccess }: { onSuccess?: (token: string) => void },
      ref: React.Ref<{ reset: () => void }>
    ) {
      react.useImperativeHandle(ref, () => ({
        reset: () => {
          mockTurnstileReset()
          // The real widget re-solves the challenge asynchronously. Issuing the
          // token synchronously here would have it wiped by the
          // setCaptchaToken('') that follows reset() in the page's effect.
          freshTokenSeq += 1
          const token = `fresh-turnstile-token-${freshTokenSeq}`
          setTimeout(() => onSuccess?.(token), 0)
        },
      }))
      return (
        <button
          type="button"
          data-testid="turnstile"
          onClick={() => onSuccess?.('mock-turnstile-token')}
        >
          Complete Turnstile
        </button>
      )
    }),
  }
})

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

  // The spent token is what makes a retry hopeless: siteverify has already
  // consumed it, so the widget has to be re-run before the button comes back.
  it('resets the captcha and retries with a fresh token after a failure', async () => {
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
    expect(mockTurnstileReset).toHaveBeenCalledTimes(1)

    // Re-enabled only once the replacement token has arrived.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Schedule migration/i })).toBeEnabled()
    })

    await userEvent.click(screen.getByRole('button', { name: /Schedule migration/i }))

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledTimes(2)
    })
    const retryBody = JSON.parse((global.fetch as jest.Mock).mock.calls[1][1].body)
    expect(retryBody.captchaToken).not.toBe('mock-turnstile-token')
    expect(retryBody.captchaToken).toMatch(/^fresh-turnstile-token-\d+$/)
  })
})
