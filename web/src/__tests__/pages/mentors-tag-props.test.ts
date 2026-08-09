import { getServerSideProps } from '@/pages/mentors/[tag]'
import { getAllMentors } from '@/server/mentors-data'
import type { GetServerSidePropsContext } from 'next'
import type { MentorListItem } from '@/types'

jest.mock('@/server/mentors-data', () => ({ getAllMentors: jest.fn() }))

const mockedGetAllMentors = getAllMentors as jest.MockedFunction<typeof getAllMentors>

/** A payload from an older Go API: no sessionsCount / photoStyle / updatedAt. */
function legacyMentor(): MentorListItem {
  return {
    id: 1,
    mentorId: '550e8400-e29b-41d4-a716-446655440001',
    slug: 'ada-lovelace-1',
    name: 'Ada Lovelace',
    job: 'Staff Engineer',
    workplace: 'Tech Corp',
    description: null,
    about: null,
    competencies: 'Go, PostgreSQL',
    experience: '10+',
    price: 'Free',
    tags: ['Backend'],
    menteeCount: 3,
    photo_url: null,
    sortOrder: 1,
    isVisible: true,
    isNew: false,
    calendarType: 'none',
  }
}

function ctx(tag: string): GetServerSidePropsContext {
  return {
    params: { tag },
    req: { headers: {} },
    res: { setHeader: jest.fn() },
    query: {},
    resolvedUrl: `/mentors/${tag}`,
  } as unknown as GetServerSidePropsContext
}

describe('/mentors/[tag] props', () => {
  it('omits optional card fields the API did not send, rather than setting undefined', async () => {
    mockedGetAllMentors.mockResolvedValue([legacyMentor()])

    const result = await getServerSideProps(ctx('backend'))
    const mentor = (result as { props: { mentors: Record<string, unknown>[] } }).props.mentors[0]

    // getServerSideProps rejects `undefined` anywhere in props — `next dev`
    // answers 500 with "Error serializing `.mentors[0].sessionsCount`", while
    // production hides it because JSON.stringify drops undefined. Assert on
    // key presence, not value: `toBeUndefined()` would pass either way.
    for (const key of ['sessionsCount', 'photoStyle', 'updatedAt']) {
      expect(Object.prototype.hasOwnProperty.call(mentor, key)).toBe(false)
    }
    expect(Object.values(mentor)).not.toContain(undefined)
  })

  it('keeps optional fields when the API does send them', async () => {
    mockedGetAllMentors.mockResolvedValue([
      { ...legacyMentor(), sessionsCount: 12, photoStyle: 'hero', updatedAt: '2026-07-08T00:00:00Z' },
    ])

    const result = await getServerSideProps(ctx('backend'))
    const mentor = (result as { props: { mentors: Record<string, unknown>[] } }).props.mentors[0]

    expect(mentor).toMatchObject({
      sessionsCount: 12,
      photoStyle: 'hero',
      updatedAt: '2026-07-08T00:00:00Z',
    })
  })

  it('ships only the fields a card renders', async () => {
    mockedGetAllMentors.mockResolvedValue([legacyMentor()])

    const result = await getServerSideProps(ctx('backend'))
    const mentor = (result as { props: { mentors: Record<string, unknown>[] } }).props.mentors[0]

    // competencies alone accepts up to 5,000 chars and no card reads it.
    for (const key of ['competencies', 'about', 'description', 'tags', 'menteeCount']) {
      expect(Object.prototype.hasOwnProperty.call(mentor, key)).toBe(false)
    }
  })

  it('lets a shared cache serve a known tag, and not an unknown one', async () => {
    mockedGetAllMentors.mockResolvedValue([legacyMentor()])

    // A tag page is identical for every visitor, same as `/` and `/mentors`.
    const known = ctx('backend')
    await getServerSideProps(known)
    expect(known.res.setHeader).toHaveBeenCalledWith(
      'Cache-Control',
      'public, max-age=0, s-maxage=60, stale-while-revalidate=300'
    )

    // The 404 for an unknown slug keeps Next's uncacheable default: a shared
    // cache must not pin a not-found answer for a tag that ships next week.
    const unknown = ctx('no-such-tag')
    const result = await getServerSideProps(unknown)
    expect(result).toEqual({ notFound: true })
    expect(unknown.res.setHeader).not.toHaveBeenCalled()
  })
})
