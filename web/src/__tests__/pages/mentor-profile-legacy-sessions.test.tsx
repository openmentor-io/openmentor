import { render, screen } from '@testing-library/react'
import Mentor from '@/pages/mentor/[slug]'
import type { MentorBase } from '@/types'

// Keep the page test focused: layout chrome and the portrait are covered elsewhere
jest.mock('@/components', () => ({
  __esModule: true,
  NavHeader: () => <div data-testid="nav-header" />,
  Footer: () => <div data-testid="footer" />,
  MetaHeader: () => null,
  HtmlContent: ({ html }: { html: string }) => <div dangerouslySetInnerHTML={{ __html: html }} />,
}))

jest.mock('@/components/ui/MentorPortrait', () => ({
  __esModule: true,
  default: () => <div data-testid="portrait" />,
}))

jest.mock('@/lib/analytics', () => ({
  __esModule: true,
  default: { event: jest.fn(), events: { MENTOR_PROFILE_VIEWED: 'mentor_profile_viewed' } },
}))

const baseMentor: MentorBase = {
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
  tags: [],
  menteeCount: 38,
  sessionsCount: 38,
  photo_url: null,
  sortOrder: 1,
  isVisible: true,
  isNew: false,
  calendarType: 'none',
}

function renderMentor(overrides: Partial<MentorBase> = {}): void {
  render(
    <Mentor
      mentor={{ ...baseMentor, ...overrides }}
      canonicalUrl="https://openmentor.io/mentor/ivan-petrov-7"
      ogImageUrl="https://openmentor.io/api/og/mentor?slug=ivan-petrov-7"
    />
  )
}

describe('Mentor profile — getmentor.dev session history (D28)', () => {
  it('discloses the carried-over share of the session count', () => {
    renderMentor({ legacySessionsCount: 35 })

    // Once in the desktop sidebar, once in the mobile meta line.
    expect(screen.getAllByText(/incl\. 35 on getmentor\.dev/)).toHaveLength(2)
    // The count itself stays a single combined number.
    expect(screen.getAllByText(/38 mentees/).length).toBeGreaterThan(0)
  })

  it('says nothing about getmentor.dev for a mentor who registered here', () => {
    renderMentor({ legacySessionsCount: 0 })

    expect(screen.queryByText(/getmentor\.dev/)).not.toBeInTheDocument()
  })

  it('says nothing when the API payload predates the field', () => {
    renderMentor()

    expect(screen.queryByText(/getmentor\.dev/)).not.toBeInTheDocument()
  })
})
