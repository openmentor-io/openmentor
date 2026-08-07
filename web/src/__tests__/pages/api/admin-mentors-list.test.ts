import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockAdminListMentors = jest.fn()

jest.mock('@/lib/go-api-client', () => ({
  getGoApiClient: () => ({
    adminListMentors: mockAdminListMentors,
  }),
}))

import handler from '@/pages/api/admin/mentors/index'

const COOKIE = 'admin_session=abc'

describe('api/admin/mentors', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  // The proxy's allowlist must accept every filter the Go API accepts —
  // 'deleted' (D70) was missing here once, which 400ed the whole Deleted tab
  // before the request ever reached the backend.
  it.each(['pending', 'approved', 'declined', 'deleted'])(
    'forwards the %s filter to the Go API',
    async (status) => {
      mockAdminListMentors.mockResolvedValue({ mentors: [] })
      const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
        method: 'GET',
        headers: { cookie: COOKIE },
        query: { status },
      })

      await handler(req, res)

      expect(res.statusCode).toBe(200)
      expect(mockAdminListMentors).toHaveBeenCalledWith(COOKIE, status)
    }
  )

  it('rejects an unknown filter without calling the Go API', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
      headers: { cookie: COOKIE },
      query: { status: 'everything' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(400)
    expect(mockAdminListMentors).not.toHaveBeenCalled()
  })

  it('defaults to pending when no filter is given', async () => {
    mockAdminListMentors.mockResolvedValue({ mentors: [] })
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
      headers: { cookie: COOKIE },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(200)
    expect(mockAdminListMentors).toHaveBeenCalledWith(COOKIE, 'pending')
  })
})
