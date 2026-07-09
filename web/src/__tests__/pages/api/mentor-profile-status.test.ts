import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

// Mock the dependencies before importing handler
jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockMentorUpdateProfileStatus = jest.fn()

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
      mentorUpdateProfileStatus: mockMentorUpdateProfileStatus,
    }),
  }
})

// Import handler after mocks are set up
import handler from '@/pages/api/mentor/profile/status'
import { HttpError } from '@/lib/go-api-client'

describe('api/mentor/profile/status', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
    })

    await handler(req, res)

    expect(res.statusCode).toBe(405)
    expect(res._getJSONData()).toEqual({ error: 'Method not allowed' })
  })

  it('returns 401 when no session cookie is present', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { status: 'inactive' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(401)
    expect(res._getJSONData()).toEqual({ error: 'Unauthorized' })
    expect(mockMentorUpdateProfileStatus).not.toHaveBeenCalled()
  })

  it.each(['pending', 'declined', '', undefined])(
    'returns 400 for invalid status %p',
    async (status) => {
      const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
        method: 'POST',
        headers: { cookie: 'mentor_session=token' },
        body: { status },
      })

      await handler(req, res)

      expect(res.statusCode).toBe(400)
      expect(res._getJSONData()).toEqual({ error: 'Invalid status' })
      expect(mockMentorUpdateProfileStatus).not.toHaveBeenCalled()
    }
  )

  it.each(['active', 'inactive'] as const)(
    'forwards %s status change to the Go API with the session cookie',
    async (status) => {
      const mockResponse = { success: true, status }
      mockMentorUpdateProfileStatus.mockResolvedValue(mockResponse)

      const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
        method: 'POST',
        headers: { cookie: 'mentor_session=token' },
        body: { status },
      })

      await handler(req, res)

      expect(mockMentorUpdateProfileStatus).toHaveBeenCalledWith('mentor_session=token', {
        status,
      })
      expect(res.statusCode).toBe(200)
      expect(res._getJSONData()).toEqual(mockResponse)
    }
  )

  it('propagates Go API error status and body (e.g. pending mentor rejected)', async () => {
    mockMentorUpdateProfileStatus.mockRejectedValue(
      new HttpError(
        403,
        'Forbidden',
        JSON.stringify({ error: 'Only active or inactive profiles can change visibility status' })
      )
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'mentor_session=token' },
      body: { status: 'active' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(403)
    expect(res._getJSONData()).toEqual({
      error: 'Only active or inactive profiles can change visibility status',
    })
  })

  it('returns 500 when Go API throws a generic error', async () => {
    mockMentorUpdateProfileStatus.mockRejectedValue(new Error('API connection failed'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'mentor_session=token' },
      body: { status: 'inactive' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
