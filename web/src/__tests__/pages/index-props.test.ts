import { getServerSideProps } from '@/pages/index'
import { getAllMentors } from '@/server/mentors-data'
import type { GetServerSidePropsContext } from 'next'
import type { MentorListItem } from '@/types'

jest.mock('@/server/mentors-data', () => ({ getAllMentors: jest.fn() }))

const mockedGetAllMentors = getAllMentors as jest.MockedFunction<typeof getAllMentors>

/**
 * A mentor shaped like the production payload: field lengths are the medians
 * measured across the live catalog, so the byte budgets below mean something.
 * `description`/`about` are null because the page fetches with
 * `drop_long_fields: true`.
 */
function mentor(index: number): MentorListItem {
  return {
    id: 1000 + index,
    mentorId: `550e8400-e29b-41d4-a716-4466554${String(index).padStart(5, '0')}`,
    slug: `mentor-name-${index}`,
    name: 'Alexandra Kuznetsova',
    job: 'Senior Software Engineer',
    workplace: 'A Reasonably Named Company',
    description: null,
    about: null,
    competencies: 'Go, PostgreSQL, Kubernetes, distributed systems, code review, career growth',
    experience: '10+',
    price: '$120 / hour',
    tags: ['Backend', 'System Design'],
    menteeCount: 7,
    sessionsCount: 12,
    legacySessionsCount: 3,
    photo_url: null,
    sortOrder: index,
    isVisible: true,
    isNew: false,
    calendarType: 'calendly',
    updatedAt: '2026-07-08T11:22:33.444Z',
    status: 'active',
    photoStyle: 'hero',
  }
}

function ctx(): GetServerSidePropsContext & { res: { setHeader: jest.Mock } } {
  return {
    params: {},
    query: {},
    req: { headers: {} },
    res: { setHeader: jest.fn() },
    resolvedUrl: '/',
  } as unknown as GetServerSidePropsContext & { res: { setHeader: jest.Mock } }
}

async function propsFor(mentors: MentorListItem[]): Promise<{
  pageMentors: Record<string, unknown>[]
  headers: jest.Mock
}> {
  mockedGetAllMentors.mockResolvedValue(mentors)
  const context = ctx()
  const result = await getServerSideProps(context)
  return {
    pageMentors: (result as { props: { pageMentors: Record<string, unknown>[] } }).props.pageMentors,
    headers: context.res.setHeader,
  }
}

/** Bytes of `__NEXT_DATA__` the catalog contributes, per mentor. */
function bytesPerMentor(mentors: unknown[]): number {
  return Buffer.byteLength(JSON.stringify(mentors), 'utf8') / mentors.length
}

describe('/ props', () => {
  const EXPECTED_KEYS = [
    'competencies',
    'experience',
    'id',
    'isNew',
    'job',
    'menteeCount',
    'mentorId',
    'name',
    'photoStyle',
    'price',
    'sessionsCount',
    'slug',
    'tags',
    'updatedAt',
    'workplace',
  ]

  it('ships only the fields the catalog renders, filters and searches', async () => {
    const { pageMentors } = await propsFor([mentor(1)])

    expect(Object.keys(pageMentors[0]).sort()).toEqual(EXPECTED_KEYS)
  })

  // These are the fields the page shipped to every visitor and read nowhere.
  it.each([
    'about',
    'calendarType',
    'calendarUrl',
    'description',
    'isVisible',
    'legacySessionsCount',
    'photo_url',
    'sortOrder',
    'status',
  ])('drops %s', async (key) => {
    const { pageMentors } = await propsFor([mentor(1)])

    expect(Object.prototype.hasOwnProperty.call(pageMentors[0], key)).toBe(false)
  })

  it('omits optional card fields the API did not send, rather than setting undefined', async () => {
    const legacy = { ...mentor(1) }
    delete legacy.sessionsCount
    delete legacy.photoStyle
    delete legacy.updatedAt

    const { pageMentors } = await propsFor([legacy])

    // getServerSideProps rejects `undefined` anywhere in props — `next dev`
    // answers 500 with "Error serializing `.pageMentors[0].sessionsCount`",
    // while production hides it because JSON.stringify drops undefined. Assert
    // on key presence, not value: `toBeUndefined()` would pass either way.
    for (const key of ['sessionsCount', 'photoStyle', 'updatedAt']) {
      expect(Object.prototype.hasOwnProperty.call(pageMentors[0], key)).toBe(false)
    }
    expect(Object.values(pageMentors[0])).not.toContain(undefined)
  })

  /**
   * The homepage ships the WHOLE catalog so filters and search run without a
   * round trip, so per-mentor bytes are the number that matters: they multiply
   * by the catalog size for every visitor. The live payload measured 730
   * B/mentor before projection and 587 after; this fixture, being uniform, is
   * 613 and 465. The ceiling is set just above the projected fixture so that
   * widening MentorCatalogItem by even one field has to come here and argue for
   * the bytes — which is the check `largePageDataBytes` cannot do, since it
   * fires only in dev and only past a whole-page threshold.
   */
  const BYTE_BUDGET_PER_MENTOR = 500

  it('holds the per-mentor payload inside its budget', async () => {
    const catalog = Array.from({ length: 300 }, (_, i) => mentor(i))
    const { pageMentors } = await propsFor(catalog)

    expect(bytesPerMentor(pageMentors)).toBeLessThan(BYTE_BUDGET_PER_MENTOR)
  })

  it('is materially smaller than the unprojected payload', async () => {
    const catalog = Array.from({ length: 300 }, (_, i) => mentor(i))
    const { pageMentors } = await propsFor(catalog)

    const saved = 1 - bytesPerMentor(pageMentors) / bytesPerMentor(catalog)
    expect(saved).toBeGreaterThan(0.15)
  })

  it('lets a shared cache serve the catalog', async () => {
    const { headers } = await propsFor([mentor(1)])

    // Without this Next sends `private, no-store` on every SSR response.
    expect(headers).toHaveBeenCalledWith(
      'Cache-Control',
      'public, max-age=0, s-maxage=60, stale-while-revalidate=300'
    )
  })
})
