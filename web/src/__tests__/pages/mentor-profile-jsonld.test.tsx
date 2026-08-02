import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import Mentor from '@/pages/mentor/[slug]'
import constants from '@/config/constants'
import type { MentorBase } from '@/types'

// next/head drops its children in jsdom, so the structured data would never
// reach the DOM. Render them inline instead — this test is about the schema.
jest.mock('next/head', () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

// Chrome is covered elsewhere; the structured data is what's under test.
jest.mock('@/components', () => ({
  __esModule: true,
  NavHeader: () => <div />,
  Footer: () => <div />,
  MetaHeader: () => null,
  HtmlContent: () => <div />,
}))

jest.mock('@/components/ui/MentorPortrait', () => ({
  __esModule: true,
  default: () => <div />,
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
  tags: ['Backend', 'System Design'],
  menteeCount: 38,
  photo_url: null,
  sortOrder: 1,
  isVisible: true,
  isNew: false,
  calendarType: 'none',
}

const CANONICAL = 'https://openmentor.io/mentor/ivan-petrov-7'

function renderMentor(overrides: Partial<MentorBase> = {}): Record<string, unknown>[] {
  const { container } = render(
    <Mentor
      mentor={{ ...baseMentor, ...overrides }}
      canonicalUrl={CANONICAL}
      ogImageUrl="https://openmentor.io/api/og/mentor?slug=ivan-petrov-7"
    />
  )

  return Array.from(container.querySelectorAll('script[type="application/ld+json"]')).map(
    (el) => JSON.parse(el.innerHTML) as Record<string, unknown>
  )
}

function byType(blocks: Record<string, unknown>[], type: string): Record<string, unknown> {
  const found = blocks.find((b) => b['@type'] === type)
  expect(found).toBeDefined()
  return found as Record<string, unknown>
}

describe('Mentor profile — structured data', () => {
  it('describes the mentor as a Person on a ProfilePage', () => {
    const profile = byType(renderMentor(), 'ProfilePage')
    const person = profile.mainEntity as Record<string, unknown>

    expect(person).toMatchObject({
      '@type': 'Person',
      name: 'Ivan Petrov',
      jobTitle: 'Staff Engineer',
      worksFor: { '@type': 'Organization', name: 'Tech Corp' },
      knowsAbout: ['Backend', 'System Design'],
      url: CANONICAL,
    })
    expect(person.description).toEqual(expect.stringContaining('Ivan Petrov'))
  })

  it('never claims a price or a rating — neither exists as structured data (D3)', () => {
    const person = byType(renderMentor(), 'ProfilePage').mainEntity as Record<string, unknown>

    expect(person.offers).toBeUndefined()
    expect(person.priceRange).toBeUndefined()
    expect(person.aggregateRating).toBeUndefined()
    expect(person.review).toBeUndefined()
  })

  it('omits knowsAbout rather than emitting an empty list', () => {
    const person = byType(renderMentor({ tags: [] }), 'ProfilePage').mainEntity as Record<
      string,
      unknown
    >

    expect(person).not.toHaveProperty('knowsAbout')
  })

  it('emits a Home -> mentor breadcrumb trail', () => {
    const crumbs = byType(renderMentor(), 'BreadcrumbList')

    expect(crumbs.itemListElement).toEqual([
      { '@type': 'ListItem', position: 1, name: 'Home', item: constants.BASE_URL },
      { '@type': 'ListItem', position: 2, name: 'Ivan Petrov', item: CANONICAL },
    ])
  })
})
