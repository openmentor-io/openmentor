import { render, screen, fireEvent } from '@testing-library/react'
import MentorsList from '@/components/mentors/MentorsList'
import { mentorPastelClass } from '@/lib/mentor-pastel'
import type { MentorListItem } from '@/types'

// Mock next/image - filter out Next.js-specific props
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
    // Suppress unused variable warnings
    void fill
    void unoptimized
    void blurDataURL
    void placeholder
    void sizes
    // eslint-disable-next-line @next/next/no-img-element
    return <img alt={alt} {...props} />
  },
}))

// Mock next/link
jest.mock('next/link', () => ({
  __esModule: true,
  default: function MockLink({
    children,
    href,
    className,
  }: {
    children: React.ReactNode
    href: string
    className?: string
  }) {
    return (
      <a href={href} className={className}>
        {children}
      </a>
    )
  },
}))

// Mock image-loader
jest.mock('@/lib/image-loader', () => ({
  imageLoader: ({ src, quality }: { src: string; quality: string }) =>
    `https://storage.example.com/${src}-${quality}.jpg`,
  updatedAtToVersion: () => 'v1',
}))

const mockMentors: MentorListItem[] = [
  {
    id: 1,
    mentorId: 'rec1',
    slug: 'john-doe',
    name: 'John Doe',
    job: 'Senior Developer',
    workplace: 'Tech Corp',
    description: 'Expert in React',
    about: 'Full bio',
    competencies: 'React, TS',
    experience: '10+',
    price: '$100',
    tags: ['Frontend'],
    menteeCount: 15,
    photo_url: null,
    sortOrder: 1,
    isVisible: true,
    isNew: false,
    calendarType: 'calendly',
  },
  {
    id: 2,
    mentorId: 'rec2',
    slug: 'jane-smith',
    name: 'Jane Smith',
    job: 'Tech Lead',
    workplace: 'StartupXYZ',
    description: 'Backend expert',
    about: 'Backend specialist',
    competencies: 'Go',
    experience: '5-10',
    price: '$50',
    tags: ['Backend'],
    menteeCount: 0,
    photo_url: null,
    sortOrder: 2,
    isVisible: true,
    isNew: true,
    calendarType: 'koalendar',
  },
]

describe('MentorsList', () => {
  it('renders list of mentors with first names', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('John')).toBeInTheDocument()
    expect(screen.getByText('Jane')).toBeInTheDocument()
  })

  it('displays mentor role as the card headline', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('Senior Developer')).toBeInTheDocument()
    expect(screen.getByText('Tech Lead')).toBeInTheDocument()
  })

  it('displays experience and price meta line when sessionsCount is absent', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('10+ years · $100')).toBeInTheDocument()
    expect(screen.getByText('5-10 years · $50')).toBeInTheDocument()
  })

  it('displays completed sessions in the meta line when sessionsCount > 0', () => {
    const withSessions = [
      { ...mockMentors[0], sessionsCount: 4 },
      { ...mockMentors[1], sessionsCount: 1 },
    ]
    render(<MentorsList mentors={withSessions} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('4 sessions · $100')).toBeInTheDocument()
    expect(screen.getByText('1 session · $50')).toBeInTheDocument()
    expect(screen.queryByText(/years/)).not.toBeInTheDocument()
  })

  it('falls back to experience when sessionsCount is 0', () => {
    const withZeroSessions = [{ ...mockMentors[0], sessionsCount: 0 }]
    render(<MentorsList mentors={withZeroSessions} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('10+ years · $100')).toBeInTheDocument()
  })

  it('does not display mentee count on cards', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.queryByText(/mentee/)).not.toBeInTheDocument()
  })

  it('assigns each mentor a deterministic pastel card background', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    const johnLink = screen.getByRole('link', { name: /John Doe/i })
    const janeLink = screen.getByRole('link', { name: /Jane Smith/i })

    for (const cls of mentorPastelClass('john-doe').split(' ')) {
      expect(johnLink).toHaveClass(cls)
    }
    for (const cls of mentorPastelClass('jane-smith').split(' ')) {
      expect(janeLink).toHaveClass(cls)
    }
  })

  it('keeps a mentor color stable across re-renders', () => {
    const { unmount } = render(
      <MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />
    )
    const before = screen.getByRole('link', { name: /John Doe/i }).className
    unmount()

    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)
    expect(screen.getByRole('link', { name: /John Doe/i }).className).toBe(before)
  })

  it('shows "New" badge for new mentors', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    // Jane is new; John is not
    expect(screen.getAllByText('New')).toHaveLength(1)
  })

  it('creates links to mentor detail pages', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    const johnLink = screen.getByRole('link', { name: /John Doe/i })
    expect(johnLink).toHaveAttribute('href', '/mentor/john-doe')

    const janeLink = screen.getByRole('link', { name: /Jane Smith/i })
    expect(janeLink).toHaveAttribute('href', '/mentor/jane-smith')
  })

  it('shows "Load more" button when hasMore is true', () => {
    render(<MentorsList mentors={mockMentors} hasMore={true} onClickMore={() => {}} />)

    expect(screen.getByRole('button', { name: /Show more/i })).toBeInTheDocument()
  })

  it('hides "Load more" button when hasMore is false', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.queryByRole('button', { name: /Show more/i })).not.toBeInTheDocument()
  })

  it('calls onClickMore when "Load more" button is clicked', () => {
    const mockOnClickMore = jest.fn()
    render(<MentorsList mentors={mockMentors} hasMore={true} onClickMore={mockOnClickMore} />)

    const button = screen.getByRole('button', { name: /Show more/i })
    fireEvent.click(button)

    expect(mockOnClickMore).toHaveBeenCalledTimes(1)
  })

  it('renders empty grid when no mentors provided', () => {
    render(<MentorsList mentors={[]} hasMore={false} onClickMore={() => {}} />)

    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
