import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockAdminUpdateMentorRequestStatus = jest.fn()

jest.mock('@/lib/go-api-client', () => {
  class HttpError extends Error {
    statusCode: number
    statusText: string
    body: string

    constructor(statusCode: number, statusText: string, body: string) {
      super(`Go API error: ${statusCode} ${statusText} - ${body}`)
      this.name = 'HttpError'
      this.statusCode = statusCode
      this.statusText = statusText
      this.body = body
    }
  }

  return {
    HttpError,
    getGoApiClient: () => ({
      adminUpdateMentorRequestStatus: mockAdminUpdateMentorRequestStatus,
    }),
  }
})

import handler from '@/pages/api/admin/mentors/[id]/requests/[requestId]/status'
import { HttpError } from '@/lib/go-api-client'

const query = { id: 'mentor-1', requestId: 'req-1' }

describe('api/admin/mentors/[id]/requests/[requestId]/status', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET', query })

    await handler(req, res)

    expect(res.statusCode).toBe(405)
    expect(mockAdminUpdateMentorRequestStatus).not.toHaveBeenCalled()
  })

  it('returns 401 when no admin session cookie is present', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query,
      body: { status: 'done' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(401)
    expect(mockAdminUpdateMentorRequestStatus).not.toHaveBeenCalled()
  })

  it.each(['archived', '', undefined, null])('returns 400 for invalid status %p', async (status) => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'admin_session=token' },
      query,
      body: { status },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(400)
    expect(mockAdminUpdateMentorRequestStatus).not.toHaveBeenCalled()
  })

  // The admin override accepts every status, terminal ones included — that is
  // the difference from the mentor-facing endpoint.
  it.each(['pending', 'contacted', 'working', 'done', 'declined', 'unavailable'] as const)(
    'forwards %s to the Go API with the admin session cookie',
    async (status) => {
      const updated = { id: 'req-1', mentorId: 'mentor-1', status }
      mockAdminUpdateMentorRequestStatus.mockResolvedValue(updated)

      const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
        method: 'POST',
        headers: { cookie: 'admin_session=token' },
        query,
        body: { status },
      })

      await handler(req, res)

      expect(mockAdminUpdateMentorRequestStatus).toHaveBeenCalledWith(
        'admin_session=token',
        'mentor-1',
        'req-1',
        status
      )
      expect(res.statusCode).toBe(200)
      expect(res._getJSONData()).toEqual({ request: updated })
    }
  )

  it('propagates the Go API error status and body (moderator role rejected)', async () => {
    mockAdminUpdateMentorRequestStatus.mockRejectedValue(
      new HttpError(403, 'Forbidden', JSON.stringify({ error: 'Access denied' }))
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'admin_session=token' },
      query,
      body: { status: 'working' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(403)
    expect(res._getJSONData()).toEqual({ error: 'Access denied' })
  })
})
