/**
 * H4 SENTINELS for the review page.
 *
 * The review capability arrives in a URL fragment, and this page is the only
 * place in the frontend that ever holds it. These tests pin the three properties
 * that keep it out of telemetry: it leaves the address bar immediately, it never
 * reaches the DOM (session replay serializes the DOM), and it never reaches an
 * analytics property. Plus the dual-read behaviour for links already sent.
 */

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import NewReviewPage, { REVIEW_TOKEN_FRAGMENT_KEY } from '@/pages/reviews/new'

// Fabricated, shaped like a real capability from api/pkg/reviewtoken.
const REVIEW_TOKEN = 'rvw_SENTINELreviewPageCapabilityAAAAAAAAAAAAAAA'
const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'

const mockRouter = { isReady: true, query: {} as Record<string, string | string[]> }
jest.mock('next/router', () => ({
  useRouter: () => mockRouter,
}))

const mockAnalyticsEvent = jest.fn()
jest.mock('@/lib/analytics', () => ({
  __esModule: true,
  default: {
    events: { REVIEW_PAGE_VIEWED: 'review_page_viewed' },
    event: (name: string, params?: Record<string, unknown>) => mockAnalyticsEvent(name, params),
  },
}))

// Turnstile renders an iframe to Cloudflare; stub it and hand the form a token.
jest.mock('@marsidev/react-turnstile', () => ({
  Turnstile: ({ onSuccess }: { onSuccess: (token: string) => void }) => {
    onSuccess('captcha-ok')
    return <div data-testid="turnstile" />
  },
}))

function mockFetchResponse(status: number, body: unknown): void {
  ;(global.fetch as jest.Mock).mockResolvedValueOnce({
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  })
}

/** Puts the capability where the email link puts it. */
function visitWithFragment(token: string): void {
  window.history.replaceState(
    null,
    '',
    `/reviews/new#${REVIEW_TOKEN_FRAGMENT_KEY}=${encodeURIComponent(token)}`
  )
}

function lastFetchBody(): Record<string, unknown> {
  const calls = (global.fetch as jest.Mock).mock.calls
  const [, init] = calls[calls.length - 1]
  return JSON.parse(init.body as string) as Record<string, unknown>
}

describe('Review page capability handling', () => {
  beforeEach(() => {
    jest.clearAllMocks()
    mockRouter.query = {}
    global.fetch = jest.fn()
    window.history.replaceState(null, '', '/reviews/new')
  })

  it('takes the token out of the address bar before anything can read it', async () => {
    visitWithFragment(REVIEW_TOKEN)
    mockFetchResponse(200, { canSubmit: true, mentorName: 'Jane Doe' })

    render(<NewReviewPage />)

    // Cleared synchronously in the first effect, which React flushes before
    // _app's analytics initialization.
    expect(window.location.hash).toBe('')
    expect(window.location.href).not.toContain(REVIEW_TOKEN)

    await waitFor(() => expect(screen.getByText(/Jane Doe/)).toBeInTheDocument())
    expect(document.body.innerHTML).not.toContain(REVIEW_TOKEN)
  })

  it('sends the token in the check request body, not its URL', async () => {
    visitWithFragment(REVIEW_TOKEN)
    mockFetchResponse(200, { canSubmit: true, mentorName: 'Jane Doe' })

    render(<NewReviewPage />)

    await waitFor(() => expect(global.fetch).toHaveBeenCalled())
    const [url, init] = (global.fetch as jest.Mock).mock.calls[0]
    expect(url).toBe('/api/reviews/check')
    expect(url).not.toContain(REVIEW_TOKEN)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ token: REVIEW_TOKEN })
  })

  it('sends the token in the submit request body and never in the DOM', async () => {
    visitWithFragment(REVIEW_TOKEN)
    mockFetchResponse(200, { canSubmit: true, mentorName: 'Jane Doe' })

    render(<NewReviewPage />)
    await waitFor(() => expect(screen.getByText(/Jane Doe/)).toBeInTheDocument())

    // No form field carries it — a hidden input would be recorded by replay.
    for (const input of Array.from(document.querySelectorAll('input, textarea'))) {
      expect((input as HTMLInputElement).value).not.toContain(REVIEW_TOKEN)
    }

    mockFetchResponse(200, { success: true, reviewId: 'rev-1' })
    await userEvent.type(
      screen.getByLabelText(/Review of the mentor/i),
      'A genuinely useful hour of advice.'
    )
    await userEvent.click(screen.getByRole('button', { name: /Submit review/i }))

    await waitFor(() => expect(screen.getByText(/Thank you for your review/i)).toBeInTheDocument())
    const [url, init] = (global.fetch as jest.Mock).mock.calls[1]
    expect(url).toBe('/api/reviews/submit')
    expect(url).not.toContain(REVIEW_TOKEN)
    expect(lastFetchBody()).toMatchObject({ token: REVIEW_TOKEN })
    expect(lastFetchBody()).not.toHaveProperty('requestId')
    expect(init.method).toBe('POST')
  })

  it('never puts the capability in an analytics property', async () => {
    visitWithFragment(REVIEW_TOKEN)
    mockFetchResponse(200, { canSubmit: true, mentorName: 'Jane Doe' })

    render(<NewReviewPage />)

    await waitFor(() => expect(mockAnalyticsEvent).toHaveBeenCalled())
    const [name, properties] = mockAnalyticsEvent.mock.calls[0]
    expect(name).toBe('review_page_viewed')
    expect(JSON.stringify(properties)).not.toContain(REVIEW_TOKEN)
    // Sanitized facts only: a boolean and an enum.
    expect(properties).toEqual({ has_request_id: false, link_type: 'token' })
  })

  // Dual-read: a link already sitting in a mentee's inbox still works.
  it('falls back to the legacy request id link', async () => {
    mockRouter.query = { request_id: REQUEST_ID }
    window.history.replaceState(null, '', `/reviews/new?request_id=${REQUEST_ID}`)
    mockFetchResponse(200, { canSubmit: true, mentorName: 'Jane Doe' })

    render(<NewReviewPage />)

    await waitFor(() => expect(global.fetch).toHaveBeenCalled())
    expect(JSON.parse((global.fetch as jest.Mock).mock.calls[0][1].body as string)).toEqual({
      requestId: REQUEST_ID,
    })
    // The legacy id is stripped from the address bar too (M10).
    expect(window.location.search).toBe('')

    const [, properties] = mockAnalyticsEvent.mock.calls[0]
    expect(properties).toEqual({ has_request_id: true, link_type: 'legacy_request_id' })
  })

  it('shows a generic message with no mentor name for a spent link', async () => {
    visitWithFragment(REVIEW_TOKEN)
    mockFetchResponse(404, {
      canSubmit: false,
      error: 'This review link has already been used or has expired.',
    })

    render(<NewReviewPage />)

    await waitFor(() =>
      expect(screen.getByText(/already been used or has expired/i)).toBeInTheDocument()
    )
    expect(screen.queryByText(/Jane Doe/)).not.toBeInTheDocument()
  })

  it('refuses to call the API with no link at all', async () => {
    render(<NewReviewPage />)

    await waitFor(() => expect(screen.getByText(/link is not valid/i)).toBeInTheDocument())
    expect(global.fetch).not.toHaveBeenCalled()
  })
})
