import { getServerSideProps } from '@/pages/mentors/index'
import { getAllMentors } from '@/server/mentors-data'
import type { GetServerSidePropsContext } from 'next'
import type { MentorListItem } from '@/types'

jest.mock('@/server/mentors-data', () => ({ getAllMentors: jest.fn() }))

const mockedGetAllMentors = getAllMentors as jest.MockedFunction<typeof getAllMentors>

function mentor(): MentorListItem {
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

describe('/mentors props', () => {
  it('lets a shared cache serve the directory', async () => {
    mockedGetAllMentors.mockResolvedValue([mentor()])
    const setHeader = jest.fn()
    const context = {
      params: {},
      query: {},
      req: { headers: {} },
      res: { setHeader },
      resolvedUrl: '/mentors',
    } as unknown as GetServerSidePropsContext

    await getServerSideProps(context)

    // The directory is identical for every visitor; without this Next sends
    // `private, no-store` and a crawler-heavy page re-renders per request.
    expect(setHeader).toHaveBeenCalledWith(
      'Cache-Control',
      'public, max-age=0, s-maxage=60, stale-while-revalidate=300'
    )
  })
})
