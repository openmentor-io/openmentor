import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import OrderMentor from '@/pages/mentor/[slug]/contact'
import type { MentorBase } from '@/types'

jest.mock('@marsidev/react-turnstile', () => {
  const react = jest.requireActual('react')
  return {
    __esModule: true,
    Turnstile: react.forwardRef(function MockTurnstile(
      { onSuccess }: { onSuccess?: (token: string) => void },
      ref: React.Ref<{ reset: () => void }>
    ) {
      react.useImperativeHandle(ref, () => ({
        reset: () => setTimeout(() => onSuccess?.('fresh-turnstile-token'), 0),
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

// Chrome is stubbed, but the contact form itself is the real component: what is
// under test is whether the upstream reason survives all the way to the panel.
jest.mock('@/components', () => ({
  __esModule: true,
  ...(jest.requireActual('@/components') as Record<string, unknown>),
  NavHeader: () => <div />,
  Footer: () => <div />,
  NoIndexHead: () => null,
  Koalendar: () => <div />,
  CalendlabWidget: () => <div />,
}))

jest.mock('react-calendly', () => ({
  __esModule: true,
  InlineWidget: () => <div data-testid="calendly" />,
}))

jest.mock('@/lib/analytics', () => ({
  __esModule: true,
  default: {
    event: jest.fn(),
    events: {
      MENTOR_CONTACT_PAGE_VIEWED: 'mentor_contact_page_viewed',
      MENTEE_CONTACT_SUBMITTED: 'mentee_contact_submitted',
    },
  },
}))

jest.mock('@/lib/posthog', () => ({
  __esModule: true,
  captureException: jest.fn(),
}))

const mentor: MentorBase = {
  id: 7,
  mentorId: '550e8400-e29b-41d4-a716-446655440000',
  slug: 'ivan-petrov-7',
  name: 'Ivan Petrov',
  job: 'Staff Engineer',
  workplace: 'Tech Corp',
  description: null,
  about: null,
  competencies: 'Go, PostgreSQL',
  experience: '10+',
  price: '$100',
  tags: ['Backend'],
  menteeCount: 38,
  photo_url: null,
  sortOrder: 1,
  isVisible: true,
  isNew: false,
  calendarType: 'none',
}

/** Fill the real form and send it, so the page runs its own fetch handling. */
async function submitContactForm(): Promise<void> {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText(/Your email/i), 'mentee@example.com')
  await user.type(screen.getByLabelText(/Your full name/i), 'Jane Mentee')
  await user.type(
    screen.getByLabelText(/What would you like to talk about/i),
    'I would like help planning a move into platform engineering.'
  )
  await user.click(screen.getByTestId('turnstile'))
  await user.click(screen.getByRole('button', { name: /Send request/i }))
}

function respondWith(status: number, body: unknown): void {
  global.fetch = jest.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => {
      if (body === undefined) throw new SyntaxError('Unexpected token < in JSON')
      return body
    },
  })
}

describe('Mentor contact page error surface', () => {
  beforeEach(() => {
    jest.clearAllMocks()
    window.localStorage.clear()
  })

  // The route forwards the upstream 4xx body; if the page drops it, the mentee
  // is told "something went wrong" about a problem they could have fixed.
  it.each([
    [400, 'Captcha verification failed', 'Captcha verification failed.'],
    [400, 'Validation failed', 'Validation failed.'],
    [429, 'Too many requests, try again later', 'Too many requests, try again later.'],
  ])('shows the upstream %i reason to the mentee', async (status, error, expected) => {
    respondWith(status, { success: false, error })
    render(<OrderMentor mentor={mentor} />)

    await submitContactForm()

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(expected))
    expect(screen.queryByText(/Something went wrong/i)).not.toBeInTheDocument()
    // Still retryable: the form stays mounted with a live submit button.
    expect(screen.getByRole('button', { name: /Send request/i })).toBeEnabled()
  })

  it.each([
    [500, { error: 'Internal server error' }],
    [502, { error: 'pq: password authentication failed' }],
    [504, undefined], // gateway HTML: res.json() rejects
  ])('never renders a %i body', async (status, body) => {
    respondWith(status, body)
    render(<OrderMentor mentor={mentor} />)

    await submitContactForm()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByRole('alert')).toHaveTextContent(/Something went wrong/i)
    expect(screen.getByRole('alert')).not.toHaveTextContent(/server error|password/i)
  })

  it('still shows the success state on a 200', async () => {
    respondWith(200, { success: true, requestId: 'req-42' })
    render(<OrderMentor mentor={mentor} />)

    await submitContactForm()

    await waitFor(() => expect(screen.getByText(/Request sent/i)).toBeInTheDocument())
    expect(screen.getByText(/req-42/)).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('falls back to the generic message when the network call fails outright', async () => {
    global.fetch = jest.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    render(<OrderMentor mentor={mentor} />)

    await submitContactForm()

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/Something went wrong/i)
    )
  })
})
