import { render, screen, fireEvent, within } from '@testing-library/react'
import Home from '@/pages/index'
import type { MentorCatalogItem } from '@/types'

// Mock next/image - filter out Next.js-specific props (same pattern as
// MentorsList.test.tsx).
jest.mock('next/image', () => ({
  __esModule: true,
  default: function MockImage({
    alt,
    fill,
    unoptimized,
    blurDataURL,
    placeholder,
    sizes,
    ...props
  }: {
    alt: string
    fill?: boolean
    unoptimized?: boolean
    blurDataURL?: string
    placeholder?: string
    sizes?: string
    [key: string]: unknown
  }) {
    void fill
    void unoptimized
    void blurDataURL
    void placeholder
    void sizes
    // eslint-disable-next-line @next/next/no-img-element
    return <img alt={alt} {...props} />
  },
}))

jest.mock('next/link', () => ({
  __esModule: true,
  default: function MockLink({
    children,
    href,
    className,
    style,
    onClick,
  }: {
    children: React.ReactNode
    href: string
    className?: string
    style?: React.CSSProperties
    onClick?: React.MouseEventHandler<HTMLAnchorElement>
  }) {
    return (
      <a href={href} className={className} style={style} onClick={onClick}>
        {children}
      </a>
    )
  },
}))

jest.mock('@/lib/image-loader', () => ({
  imageLoader: ({ src, quality }: { src: string; quality: string }) =>
    `https://storage.example.com/${src}-${quality}.jpg`,
  updatedAtToVersion: () => 'v1',
}))

function catalogMentor(index: number, overrides: Partial<MentorCatalogItem> = {}): MentorCatalogItem {
  return {
    id: 1000 + index,
    mentorId: `550e8400-e29b-41d4-a716-4466554${String(index).padStart(5, '0')}`,
    slug: `mentor-${index}`,
    name: `Mentor Number ${index}`,
    job: 'Software Engineer',
    workplace: 'Some Company',
    experience: '10+',
    price: '$100',
    isNew: false,
    tags: ['Backend'],
    menteeCount: 0,
    competencies: 'Go, PostgreSQL',
    sessionsCount: 0,
    ...overrides,
  }
}

describe('/ sort by sessions', () => {
  // Regression test for the bug where a mentor with the most completed
  // sessions never reached the top under "Most sessions": useMentors()
  // paginates the catalog to DEFAULT_PAGE_SIZE (48) BEFORE sortMentors() ever
  // runs, so sorting only reordered whichever 48 mentors happened to load
  // first. A high-session mentor placed beyond that cutoff (as they would be,
  // since the API orders by a randomized `sort_order`, not by session count)
  // could never surface no matter how many sessions they had.
  it('surfaces a mentor buried past the default page size once sorted by sessions', () => {
    const buriedMentor = catalogMentor(49, {
      name: 'Oleg Kozhanov',
      slug: 'kozhanov-oleg',
      sessionsCount: 86,
    })

    // 48 low-session mentors (the default page size) followed by the
    // high-session mentor arriving 49th, past the cutoff.
    const pageMentors: MentorCatalogItem[] = [
      ...Array.from({ length: 48 }, (_, i) => catalogMentor(i, { sessionsCount: 1 })),
      buriedMentor,
    ]

    render(<Home pageMentors={pageMentors} />)

    fireEvent.click(screen.getByRole('button', { name: /SORT: RELEVANCE/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Most sessions' }))

    const cards = screen.getAllByRole('link', { name: /Mentor Number|Oleg Kozhanov/ })
    expect(within(cards[0]).getByText('Oleg Kozhanov')).toBeInTheDocument()
  })
})
