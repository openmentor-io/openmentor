import { render, screen, fireEvent } from '@testing-library/react'
import MenteeRequestCard from '@/components/mentee/MenteeRequestCard'
import type { MenteeRequest } from '@/types'

jest.mock('@marsidev/react-turnstile', () => ({ Turnstile: () => null }))
jest.mock('@/lib/mentee-api', () => ({ submitMenteeReview: jest.fn() }))

function makeRequest(overrides: Partial<MenteeRequest>): MenteeRequest {
  return {
    id: '1b0c9e42-a072-47ed-8ce9-edd306874ec9',
    status: 'pending',
    group: 'pending',
    details: 'Help me prepare for interviews',
    createdAt: '2026-11-01T10:00:00Z',
    statusChangedAt: null,
    mentor: { name: 'Alice Mentor', slug: 'alice', available: true },
    review: null,
    canReview: false,
    ...overrides,
  }
}

describe('MenteeRequestCard', () => {
  it('links the calendar when the API provides one', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({
          mentor: {
            name: 'Alice Mentor',
            slug: 'alice',
            available: true,
            calendarUrl: 'https://cal.example.com/alice',
          },
        })}
        onReviewed={jest.fn()}
      />,
    )
    const link = screen.getByRole('link', { name: /book a time/i })
    expect(link).toHaveAttribute('href', 'https://cal.example.com/alice')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByRole('link', { name: 'Alice Mentor' })).toHaveAttribute(
      'href',
      '/mentor/alice',
    )
  })

  // The API already drops non-http(s) values; this is the second fence, so a
  // javascript: URL in a mentor's profile can never become a clickable link.
  it('refuses a calendar URL that is not http(s)', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({
          mentor: { name: 'Alice', available: true, calendarUrl: 'javascript:alert(1)' },
        })}
        onReviewed={jest.fn()}
      />,
    )
    expect(screen.queryByRole('link', { name: /book a time/i })).not.toBeInTheDocument()
  })

  it('shows no calendar and no review action on a closed request', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({
          status: 'declined',
          group: 'closed',
          declineReason: 'no_time',
          declineComment: 'Busy month',
        })}
        onReviewed={jest.fn()}
      />,
    )
    expect(screen.queryByRole('link', { name: /book a time/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /leave a review/i })).not.toBeInTheDocument()
    expect(screen.getByText('Not enough time right now')).toBeInTheDocument()
    expect(screen.getByText('Busy month')).toBeInTheDocument()
  })

  it('offers the review form on a reviewable completed request', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({ status: 'done', group: 'done', canReview: true })}
        onReviewed={jest.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /leave a review/i }))
    expect(screen.getByLabelText(/review of the mentor/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /submit review/i })).toBeInTheDocument()
  })

  it('shows an existing review read-only, with no way to edit it', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({
          status: 'done',
          group: 'done',
          review: {
            mentorReview: 'A genuinely useful hour',
            platformReview: '',
            improvements: '',
            createdAt: '2026-11-10T10:00:00Z',
          },
        })}
        onReviewed={jest.fn()}
      />,
    )
    expect(screen.queryByText('A genuinely useful hour')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /view your review/i }))
    expect(screen.getByText('A genuinely useful hour')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /leave a review/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('names a deleted mentor generically and offers no profile link', () => {
    render(
      <MenteeRequestCard
        request={makeRequest({ mentor: { name: '', available: false } })}
        onReviewed={jest.fn()}
      />,
    )
    expect(screen.getByText('Mentor no longer on OpenMentor')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
