import { render, screen, fireEvent } from '@testing-library/react'
import MentorsFilters from '@/components/mentors/MentorsFilters'
import allFilters from '@/config/filters'
import type { AppliedFilters, MentorListItem } from '@/types'

function makeAppliedFilters(overrides: Partial<AppliedFilters> = {}): AppliedFilters {
  return {
    tags: { values: [], set: jest.fn(), reset: jest.fn() },
    category: { values: undefined, set: jest.fn(), reset: jest.fn() },
    experience: { values: [], set: jest.fn(), reset: jest.fn() },
    price: { values: undefined, set: jest.fn(), reset: jest.fn() },
    noSessions: { value: false, set: jest.fn(), reset: jest.fn() },
    newMentor: { value: false, set: jest.fn(), reset: jest.fn() },
    count: () => 0,
    ...overrides,
  }
}

function makeMentor(overrides: Partial<MentorListItem>): MentorListItem {
  return {
    id: 1,
    mentorId: 'rec1',
    slug: 'mentor',
    name: 'Mentor',
    job: 'Dev',
    workplace: 'Corp',
    competencies: 'Things',
    experience: '2-5',
    price: '$50',
    tags: ['Frontend'],
    menteeCount: 0,
    photo_url: null,
    sortOrder: 1,
    isVisible: true,
    isNew: false,
    calendarType: 'none',
    ...overrides,
  }
}

const mentors: MentorListItem[] = [
  makeMentor({ id: 1, slug: 'a', tags: ['Frontend'], experience: '2-5', price: '$50' }),
  makeMentor({ id: 2, slug: 'b', tags: ['Frontend', 'Backend'], experience: '10+', price: 'Free' }),
  makeMentor({ id: 3, slug: 'c', tags: ['UX/UI/Design'], experience: '10+', price: 'Negotiable' }),
]

describe('MentorsFilters', () => {
  it('renders the All pill, every category pill, and the Experience/Price pills', () => {
    render(<MentorsFilters appliedFilters={makeAppliedFilters()} mentors={mentors} />)

    expect(screen.getByRole('button', { name: /All mentors/i })).toBeInTheDocument()
    for (const category of allFilters.categories) {
      expect(screen.getByRole('button', { name: category.label })).toBeInTheDocument()
    }
    expect(screen.getByRole('button', { name: 'Experience' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Price' })).toBeInTheDocument()
  })

  it('marks "All mentors" active only when no topic filter is applied', () => {
    const { rerender } = render(
      <MentorsFilters appliedFilters={makeAppliedFilters()} mentors={mentors} />
    )
    expect(screen.getByRole('button', { name: /All mentors/i })).toHaveAttribute(
      'aria-pressed',
      'true'
    )

    rerender(
      <MentorsFilters
        appliedFilters={makeAppliedFilters({
          category: { values: 'Development', set: jest.fn(), reset: jest.fn() },
        })}
        mentors={mentors}
      />
    )
    expect(screen.getByRole('button', { name: /All mentors/i })).toHaveAttribute(
      'aria-pressed',
      'false'
    )
  })

  it('activates a category (clearing any tag) and opens its dropdown with per-tag counts', () => {
    const appliedFilters = makeAppliedFilters()
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    fireEvent.click(screen.getByRole('button', { name: 'Development' }))

    expect(appliedFilters.category.set).toHaveBeenCalledWith('Development')
    expect(appliedFilters.tags.set).toHaveBeenCalledWith([])

    // Dropdown lists the category's tags with counts from the loaded mentors
    const frontendItem = screen.getByRole('button', { name: /Frontend/ })
    expect(frontendItem).toHaveTextContent('2')
    expect(screen.getByRole('button', { name: /Backend/ })).toHaveTextContent('1')
    expect(screen.getByRole('button', { name: /iOS/ })).toHaveTextContent('0')
  })

  it('narrows to a single tag from the category dropdown and closes the panel', () => {
    const appliedFilters = makeAppliedFilters({
      category: { values: 'Development', set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    // Active category click opens the dropdown
    fireEvent.click(screen.getByRole('button', { name: 'Development' }))
    fireEvent.click(screen.getByRole('button', { name: /Backend/ }))

    expect(appliedFilters.tags.set).toHaveBeenCalledWith(['Backend'])
    expect(screen.queryByRole('button', { name: /iOS/ })).not.toBeInTheDocument()
  })

  it('re-picking the selected tag widens back to the whole category', () => {
    const appliedFilters = makeAppliedFilters({
      category: { values: 'Development', set: jest.fn(), reset: jest.fn() },
      tags: { values: ['Backend'], set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    // Pill shows the narrowed tag as its label
    fireEvent.click(screen.getByRole('button', { name: 'Backend' }))
    fireEvent.click(screen.getAllByRole('button', { name: /Backend/ })[1])

    expect(appliedFilters.tags.set).toHaveBeenCalledWith([])
  })

  it('toggles the dropdown when the active category is clicked again', () => {
    const appliedFilters = makeAppliedFilters({
      category: { values: 'Development', set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    const pill = screen.getByRole('button', { name: 'Development' })

    fireEvent.click(pill)
    expect(screen.getByRole('button', { name: /Backend/ })).toBeInTheDocument()
    expect(pill).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(pill)
    expect(screen.queryByRole('button', { name: /Backend/ })).not.toBeInTheDocument()
    expect(pill).toHaveAttribute('aria-expanded', 'false')
  })

  it('toggles a single-tag category (no dropdown) straight off', () => {
    const appliedFilters = makeAppliedFilters({
      category: { values: 'Design', set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    const pill = screen.getByRole('button', { name: 'Design' })
    expect(pill).not.toHaveAttribute('aria-expanded')

    fireEvent.click(pill)

    expect(appliedFilters.category.reset).toHaveBeenCalled()
    expect(appliedFilters.tags.set).toHaveBeenCalledWith([])
  })

  it('resets category and tags when "All mentors" is clicked', () => {
    const appliedFilters = makeAppliedFilters({
      category: { values: 'Development', set: jest.fn(), reset: jest.fn() },
      tags: { values: ['Backend'], set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    fireEvent.click(screen.getByRole('button', { name: /All mentors/i }))

    expect(appliedFilters.category.reset).toHaveBeenCalled()
    expect(appliedFilters.tags.set).toHaveBeenCalledWith([])
  })

  it('multi-selects experience levels and keeps the panel open', () => {
    const appliedFilters = makeAppliedFilters({
      experience: { values: ['2-5 years'], set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    fireEvent.click(screen.getByRole('button', { name: /Experience/ }))

    // Counts come from the loaded mentors (two mentors are 10+)
    const tenPlus = screen.getByRole('button', { name: /10\+ years/ })
    expect(tenPlus).toHaveTextContent('2')

    fireEvent.click(tenPlus)
    expect(appliedFilters.experience.set).toHaveBeenCalledWith(['2-5 years', '10+ years'])

    // Panel stays open for multi-select
    expect(screen.getByRole('button', { name: /5-10 years/ })).toBeInTheDocument()

    // Deselecting removes the value
    fireEvent.click(screen.getByRole('button', { name: /2-5 years/ }))
    expect(appliedFilters.experience.set).toHaveBeenCalledWith([])
  })

  it('shows a selection count badge on the Experience pill', () => {
    const appliedFilters = makeAppliedFilters({
      experience: { values: ['2-5 years', '10+ years'], set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    expect(screen.getByRole('button', { name: /Experience/ })).toHaveTextContent('Experience2')
  })

  it('single-selects a price bucket and closes the panel', () => {
    const appliedFilters = makeAppliedFilters()
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    fireEvent.click(screen.getByRole('button', { name: 'Price' }))

    const freeItem = screen.getByRole('button', { name: /Free/ })
    expect(freeItem).toHaveTextContent('1')

    fireEvent.click(freeItem)
    expect(appliedFilters.price.set).toHaveBeenCalledWith('Free')
    expect(screen.queryByRole('button', { name: /Negotiable/ })).not.toBeInTheDocument()
  })

  it('unsets the price bucket when the selected one is picked again', () => {
    const appliedFilters = makeAppliedFilters({
      price: { values: 'Free', set: jest.fn(), reset: jest.fn() },
    })
    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    fireEvent.click(screen.getByRole('button', { name: /^Price/ }))
    fireEvent.click(screen.getByRole('button', { name: /Free/ }))

    expect(appliedFilters.price.set).toHaveBeenCalledWith(undefined)
  })

  it('initializes tags from the #tags: URL hash on mount', () => {
    const appliedFilters = makeAppliedFilters()
    window.location.hash = '#tags:Frontend|NotATag'

    render(<MentorsFilters appliedFilters={appliedFilters} mentors={mentors} />)

    expect(appliedFilters.tags.set).toHaveBeenCalledWith(['Frontend'])
    window.location.hash = ''
  })
})
