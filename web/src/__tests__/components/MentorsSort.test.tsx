import { render, screen, fireEvent } from '@testing-library/react'
import MentorsSort, { sortMentors } from '@/components/mentors/MentorsSort'
import analytics from '@/lib/analytics'
import type { MentorListItem } from '@/types'

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
  makeMentor({ id: 1, slug: 'a', sessionsCount: 3 }),
  makeMentor({ id: 2, slug: 'b', isNew: true }),
  makeMentor({ id: 3, slug: 'c', sessionsCount: 40 }),
]

describe('sortMentors', () => {
  it('keeps the default order for relevance', () => {
    expect(sortMentors(mentors, 'relevance').map((m) => m.id)).toEqual([1, 2, 3])
  })

  it('sorts by completed sessions, descending (missing counts as 0)', () => {
    expect(sortMentors(mentors, 'sessions').map((m) => m.id)).toEqual([3, 1, 2])
  })

  it('puts NEW mentors first for newest, keeping relative order otherwise', () => {
    expect(sortMentors(mentors, 'newest').map((m) => m.id)).toEqual([2, 1, 3])
  })

  it('does not mutate the input list', () => {
    const input = [...mentors]
    sortMentors(input, 'sessions')
    expect(input.map((m) => m.id)).toEqual([1, 2, 3])
  })
})

describe('MentorsSort', () => {
  it('renders the current sort in the mono control label', () => {
    render(<MentorsSort value="relevance" onChange={() => {}} />)

    expect(screen.getByRole('button', { name: /SORT: RELEVANCE/ })).toBeInTheDocument()
  })

  it('opens the dropdown and reports the picked option', () => {
    const onChange = jest.fn()
    render(<MentorsSort value="relevance" onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: /SORT: RELEVANCE/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Most sessions' }))

    expect(onChange).toHaveBeenCalledWith('sessions')
  })

  it('reflects a non-default value in the label', () => {
    render(<MentorsSort value="newest" onChange={() => {}} />)

    expect(screen.getByRole('button', { name: /SORT: NEWEST/ })).toBeInTheDocument()
  })

  it('fires mentors_sort_changed with the picked and previous option', () => {
    const eventSpy = jest.spyOn(analytics, 'event').mockImplementation(() => {})
    render(<MentorsSort value="relevance" onChange={() => {}} />)

    fireEvent.click(screen.getByRole('button', { name: /SORT: RELEVANCE/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Most sessions' }))

    expect(eventSpy).toHaveBeenCalledWith(analytics.events.MENTORS_SORT_CHANGED, {
      sort_option: 'sessions',
      previous_option: 'relevance',
    })

    eventSpy.mockRestore()
  })
})
