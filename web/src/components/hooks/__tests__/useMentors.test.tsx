import { renderHook, act } from '@testing-library/react'
import useMentors from '@/components/hooks/useMentors'
import filters from '@/config/filters'
import type { MentorListItem } from '@/types'

const baseMentors: MentorListItem[] = [
  {
    id: 1,
    mentorId: 'rec1',
    slug: 'frontend-mentor',
    name: 'Frontend Mentor',
    job: 'FE',
    workplace: 'Company',
    description: 'Helps with React',
    about: 'Experienced in frontend',
    competencies: 'React, JS',
    experience: '5-10',
    price: '$120',
    tags: ['Frontend', 'React'],
    menteeCount: 10,
    photo_url: null,
    sortOrder: 1,
    isVisible: true,
    isNew: false,
    calendarType: 'none',
  },
  {
    id: 2,
    mentorId: 'rec2',
    slug: 'backend-mentor',
    name: 'Backend Mentor',
    job: 'BE',
    workplace: 'Company',
    description: 'Helps with Go',
    about: 'Experienced in backend',
    competencies: 'Go, API',
    experience: '10+',
    price: '$45',
    tags: ['Backend'],
    menteeCount: 0,
    photo_url: null,
    sortOrder: 2,
    isVisible: true,
    isNew: true,
    calendarType: 'none',
  },
]

describe('useMentors', () => {
  it('filters by search term', () => {
    const { result } = renderHook(() => useMentors(baseMentors, 10))

    act(() => {
      result.current[3]('React') // setSearchInput
    })

    const mentors = result.current[0]
    expect(mentors).toHaveLength(1)
    expect(mentors[0].slug).toBe('frontend-mentor')
    expect(result.current[6]).toBe(1) // filteredCount
  })

  it('reports the total filtered count independently of pagination', () => {
    const { result } = renderHook(() => useMentors(baseMentors, 1))

    expect(result.current[0]).toHaveLength(1) // visible page
    expect(result.current[6]).toBe(2) // total matching
    expect(result.current[2]).toBe(true) // hasMoreMentors
  })

  it('filters by tags and experience mapping', () => {
    const { result } = renderHook(() => useMentors(baseMentors, 10))
    const experienceKey = Object.keys(filters.experience).find(
      (key) => filters.experience[key as keyof typeof filters.experience] === '10+'
    ) as string

    act(() => {
      result.current[5].tags.set(['Backend'])
      result.current[5].experience.set([experienceKey])
    })

    const mentors = result.current[0]
    expect(mentors).toHaveLength(1)
    expect(mentors[0].slug).toBe('backend-mentor')
  })

  it('filters by topic category (any matching tag) and toggles off', () => {
    const { result } = renderHook(() => useMentors(baseMentors, 10))

    act(() => {
      result.current[5].category.set('Engineering')
    })

    // Both mentors have an Engineering tag (Frontend / Backend)
    expect(result.current[0]).toHaveLength(2)

    act(() => {
      result.current[5].category.set('AI & Data')
    })

    expect(result.current[0]).toHaveLength(0)

    act(() => {
      result.current[5].category.reset()
    })

    expect(result.current[0]).toHaveLength(2)
  })

  it('filters by price and new/mentees flags with pagination', () => {
    const { result } = renderHook(() => useMentors(baseMentors, 1))

    act(() => {
      result.current[5].price.set('≤$50')
      result.current[5].newMentor.set(true)
      result.current[5].noSessions.set(true)
    })

    // initial page size 1 should include backend mentor after filters
    expect(result.current[0]).toHaveLength(1)
    expect(result.current[0][0].slug).toBe('backend-mentor')

    act(() => {
      result.current[4]() // showMoreMentors
    })

    expect(result.current[2]).toBe(false) // hasMoreMentors
  })
})

/**
 * The haystack must be exactly the text the catalog payload carries. Its only
 * caller fetches with `drop_long_fields: true`, so `description` and `about`
 * arrive empty on every request — searching them looked like bio search and
 * matched nothing (C10).
 */
describe('useMentors search haystack', () => {
  function search(mentors: MentorListItem[], term: string): string[] {
    const { result } = renderHook(() => useMentors(mentors, 10))
    act(() => {
      result.current[3](term)
    })
    return result.current[0].map((mentor) => mentor.slug)
  }

  it.each([
    ['name', 'Frontend'],
    ['job', 'FE'],
    ['workplace', 'Company'],
    ['competencies', 'JS'],
  ])('matches on %s, which the payload always carries', (_field, term) => {
    expect(search(baseMentors, term)).toContain('frontend-mentor')
  })

  // These two would match here and never in production — the failure C10 fixes.
  it.each(['description', 'about'] as const)(
    'does not pretend to search %s',
    (field) => {
      const mentors: MentorListItem[] = [
        {
          ...baseMentors[0],
          description: null,
          about: null,
          competencies: 'React',
          [field]: 'kubernetes',
        },
      ]

      expect(search(mentors, 'kubernetes')).toEqual([])
      expect(search(mentors, 'React')).toEqual(['frontend-mentor'])
    }
  )

  it('is unaffected by whether the API dropped the long fields', () => {
    const withLongFields: MentorListItem[] = baseMentors.map((mentor) => ({ ...mentor }))
    const dropped: MentorListItem[] = baseMentors.map((mentor) => ({
      ...mentor,
      description: null,
      about: null,
    }))

    for (const term of ['Frontend', 'JS', 'Go', 'Company']) {
      expect(search(withLongFields, term)).toEqual(search(dropped, term))
    }
  })
})
