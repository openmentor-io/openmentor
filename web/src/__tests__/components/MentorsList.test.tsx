import { render, screen, fireEvent, within } from '@testing-library/react'
import MentorsList from '@/components/mentors/MentorsList'
import { mentorInitialsClass, mentorPastelGradClass } from '@/lib/mentor-pastel'
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
    style,
  }: {
    children: React.ReactNode
    href: string
    className?: string
    style?: React.CSSProperties
  }) {
    return (
      <a href={href} className={className} style={style}>
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

const baseMentor: MentorListItem = {
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
}

const mockMentors: MentorListItem[] = [
  baseMentor,
  {
    ...baseMentor,
    id: 2,
    mentorId: 'rec2',
    slug: 'jane-smith',
    name: 'Jane Smith',
    job: 'Tech Lead',
    workplace: 'StartupXYZ',
    experience: '5-10',
    price: '$50',
    tags: ['Backend'],
    menteeCount: 0,
    isNew: true,
  },
]

function getCard(name: RegExp): HTMLElement {
  return screen.getByRole('link', { name })
}

describe('MentorsList', () => {
  it('renders mentor names and role · workplace lines', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.getByText('John Doe')).toBeInTheDocument()
    expect(screen.getByText('Jane Smith')).toBeInTheDocument()
    expect(screen.getByText('Senior Developer · Tech Corp')).toBeInTheDocument()
    expect(screen.getByText('Tech Lead · StartupXYZ')).toBeInTheDocument()
  })

  it('formats the experience meta ("2-5" → "2–5Y EXP", "10+" → "10+Y EXP")', () => {
    render(
      <MentorsList
        mentors={[baseMentor, { ...baseMentor, id: 2, slug: 'x', experience: '2-5' }]}
        hasMore={false}
        onClickMore={() => {}}
      />
    )

    expect(screen.getByText(/10\+Y EXP/)).toBeInTheDocument()
    expect(screen.getByText(/2–5Y EXP/)).toBeInTheDocument()
  })

  it('renders the parsed price in navy, FREE in mint ink, and NEGOTIABLE muted', () => {
    render(
      <MentorsList
        mentors={[
          baseMentor,
          { ...baseMentor, id: 2, slug: 'free-mentor', price: 'Free' },
          { ...baseMentor, id: 3, slug: 'nego-mentor', price: 'Negotiable' },
          { ...baseMentor, id: 4, slug: 'hourly-mentor', price: '$150 / hour' },
        ]}
        hasMore={false}
        onClickMore={() => {}}
      />
    )

    expect(screen.getByText('$100')).toHaveClass('text-brand-navy')
    expect(screen.getByText('$150')).toHaveClass('text-brand-navy')
    expect(screen.getByText('FREE')).toHaveClass('text-mint-ink')
    expect(screen.getByText('NEGOTIABLE')).toBeInTheDocument()
  })

  describe('photo states', () => {
    it('renders the hero cut-out (multiply blend) when photoStyle is "hero"', () => {
      render(
        <MentorsList
          mentors={[{ ...baseMentor, photo_url: 'http://example.com/p.jpg', photoStyle: 'hero' }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      const img = within(getCard(/John Doe/i)).getByRole('presentation')
      expect(img).toHaveClass('mix-blend-multiply')
      expect(img).toHaveAttribute('src', 'https://storage.example.com/john-doe-large.jpg')
    })

    it('renders the arch-masked tile (fallback A) when photoStyle is absent', () => {
      render(
        <MentorsList
          mentors={[{ ...baseMentor, photo_url: 'http://example.com/p.jpg' }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      const img = within(getCard(/John Doe/i)).getByRole('presentation')
      expect(img).toHaveClass('rounded-t-panel')
      expect(img).not.toHaveClass('mix-blend-multiply')
    })

    it('renders the arch-masked tile when photoStyle is "frame"', () => {
      render(
        <MentorsList
          mentors={[{ ...baseMentor, photo_url: 'http://example.com/p.jpg', photoStyle: 'frame' }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      expect(within(getCard(/John Doe/i)).getByRole('presentation')).toHaveClass('rounded-t-panel')
    })

    it('renders hash-colored initials (fallback B) when there is no photo', () => {
      render(<MentorsList mentors={[baseMentor]} hasMore={false} onClickMore={() => {}} />)

      const initials = screen.getByText('JD')
      expect(initials).toHaveClass(mentorInitialsClass('john-doe'))
      expect(within(getCard(/John Doe/i)).queryByRole('presentation')).not.toBeInTheDocument()
    })
  })

  it('assigns each card photo block a deterministic pastel gradient', () => {
    const { container } = render(
      <MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />
    )

    expect(container.querySelector(`.${mentorPastelGradClass('john-doe')}`)).toBeInTheDocument()
    expect(container.querySelector(`.${mentorPastelGradClass('jane-smith')}`)).toBeInTheDocument()
  })

  describe('badges', () => {
    it('shows the sessions badge when sessionsCount > 0 and mentor is not new', () => {
      render(
        <MentorsList
          mentors={[
            { ...baseMentor, sessionsCount: 23 },
            { ...baseMentor, id: 2, slug: 'one-session', sessionsCount: 1 },
          ]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      expect(screen.getByText('23 SESSIONS')).toBeInTheDocument()
      expect(screen.getByText('1 SESSION')).toBeInTheDocument()
    })

    it('shows NEW for new mentors and lets it win over the sessions badge', () => {
      render(
        <MentorsList
          mentors={[{ ...baseMentor, isNew: true, sessionsCount: 23 }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      expect(screen.getByText('NEW')).toBeInTheDocument()
      expect(screen.queryByText(/SESSIONS/)).not.toBeInTheDocument()
    })

    it('shows no badge when sessionsCount is 0 and mentor is not new', () => {
      render(
        <MentorsList
          mentors={[{ ...baseMentor, sessionsCount: 0 }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )

      expect(screen.queryByText('NEW')).not.toBeInTheDocument()
      expect(screen.queryByText(/SESSION/)).not.toBeInTheDocument()
    })
  })

  describe('entrance animation', () => {
    const manyMentors = Array.from({ length: 14 }, (_, i) => ({
      ...baseMentor,
      id: i + 1,
      slug: `mentor-${i}`,
      name: `Mentor Number${i}`,
    }))

    it('staggers the first 12 cards by 40ms on first mount only', () => {
      const { rerender } = render(
        <MentorsList mentors={manyMentors} hasMore={false} onClickMore={() => {}} />
      )

      const first = getCard(/Mentor Number0/)
      const fifth = getCard(/Mentor Number4/)
      const thirteenth = getCard(/Mentor Number12/)

      expect(first).toHaveClass('animate-rise-in')
      expect(first).toHaveStyle({ animationDelay: '0ms' })
      expect(fifth).toHaveClass('animate-rise-in')
      expect(fifth).toHaveStyle({ animationDelay: '160ms' })
      expect(thirteenth).not.toHaveClass('animate-rise-in')

      // After mount (e.g. filter change), newly rendered cards do not animate.
      rerender(
        <MentorsList
          mentors={[{ ...baseMentor, id: 99, slug: 'late-mentor', name: 'Late Mentor' }]}
          hasMore={false}
          onClickMore={() => {}}
        />
      )
      expect(getCard(/Late Mentor/)).not.toHaveClass('animate-rise-in')
    })
  })

  it('creates links to mentor detail pages', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(getCard(/John Doe/i)).toHaveAttribute('href', '/mentor/john-doe')
    expect(getCard(/Jane Smith/i)).toHaveAttribute('href', '/mentor/jane-smith')
  })

  it('shows "Show more mentors" button when hasMore is true', () => {
    render(<MentorsList mentors={mockMentors} hasMore={true} onClickMore={() => {}} />)

    expect(screen.getByRole('button', { name: /Show more mentors/i })).toBeInTheDocument()
  })

  it('hides "Show more mentors" button when hasMore is false', () => {
    render(<MentorsList mentors={mockMentors} hasMore={false} onClickMore={() => {}} />)

    expect(screen.queryByRole('button', { name: /Show more/i })).not.toBeInTheDocument()
  })

  it('calls onClickMore when "Show more mentors" is clicked', () => {
    const mockOnClickMore = jest.fn()
    render(<MentorsList mentors={mockMentors} hasMore={true} onClickMore={mockOnClickMore} />)

    fireEvent.click(screen.getByRole('button', { name: /Show more mentors/i }))

    expect(mockOnClickMore).toHaveBeenCalledTimes(1)
  })

  it('renders empty grid when no mentors provided', () => {
    render(<MentorsList mentors={[]} hasMore={false} onClickMore={() => {}} />)

    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
