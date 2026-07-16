import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

// Mock the dependencies before importing handlers
jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockMentorSubmitProfile = jest.fn()

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
      mentorSubmitProfile: mockMentorSubmitProfile,
    }),
  }
})

// Import handler after mocks are set up
import submitHandler from '@/pages/api/mentor/profile/submit'
import { HttpError } from '@/lib/go-api-client'

describe('api/mentor/profile/submit', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })

    await submitHandler(req, res)

    expect(res.statusCode).toBe(405)
    expect(res._getJSONData()).toEqual({ error: 'Method not allowed' })
  })

  it('returns 401 without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST' })

    await submitHandler(req, res)

    expect(res.statusCode).toBe(401)
    expect(res._getJSONData()).toEqual({ error: 'Unauthorized' })
    expect(mockMentorSubmitProfile).not.toHaveBeenCalled()
  })

  it('forwards the cookies to the Go API and returns its response', async () => {
    mockMentorSubmitProfile.mockResolvedValue({ success: true, status: 'pending' })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'mentor_session=abc123' },
    })

    await submitHandler(req, res)

    expect(mockMentorSubmitProfile).toHaveBeenCalledWith('mentor_session=abc123')
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true, status: 'pending' })
  })

  it('passes through the 403 not-submittable status', async () => {
    mockMentorSubmitProfile.mockRejectedValue(
      new HttpError(
        403,
        'Forbidden',
        JSON.stringify({ error: 'Only draft profiles can be submitted for review' })
      )
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'mentor_session=abc123' },
    })

    await submitHandler(req, res)

    expect(res.statusCode).toBe(403)
    expect(res._getJSONData()).toEqual({
      error: 'Only draft profiles can be submitted for review',
    })
  })

  it('returns 500 for unexpected errors', async () => {
    mockMentorSubmitProfile.mockRejectedValue(new Error('connection refused'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: 'mentor_session=abc123' },
    })

    await submitHandler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
