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

const mockAdminReturnMentor = jest.fn()

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
      adminReturnMentor: mockAdminReturnMentor,
    }),
  }
})

// Import handler after mocks are set up
import returnHandler from '@/pages/api/admin/mentors/[id]/return'
import { HttpError } from '@/lib/go-api-client'

describe('api/admin/mentors/[id]/return', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
      query: { id: 'mentor-1' },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(405)
  })

  it('returns 401 without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      body: { reason: 'Please add a photo' },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(401)
    expect(mockAdminReturnMentor).not.toHaveBeenCalled()
  })

  it('returns 400 when the reason is missing or blank', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      headers: { cookie: 'admin_session=abc123' },
      body: { reason: '   ' },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(400)
    expect(mockAdminReturnMentor).not.toHaveBeenCalled()
  })

  it('returns 400 when the reason exceeds 2000 characters', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      headers: { cookie: 'admin_session=abc123' },
      body: { reason: 'x'.repeat(2001) },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(400)
    expect(mockAdminReturnMentor).not.toHaveBeenCalled()
  })

  it('forwards cookies, mentor id and reason to the Go API', async () => {
    const mentor = { mentorId: 'mentor-1', status: 'draft', moderationNote: 'Add a photo' }
    mockAdminReturnMentor.mockResolvedValue(mentor)

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      headers: { cookie: 'admin_session=abc123' },
      body: { reason: 'Add a photo' },
    })

    await returnHandler(req, res)

    expect(mockAdminReturnMentor).toHaveBeenCalledWith('admin_session=abc123', 'mentor-1', {
      reason: 'Add a photo',
    })
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ mentor })
  })

  it('passes through the 409 already-activated conflict', async () => {
    mockAdminReturnMentor.mockRejectedValue(
      new HttpError(
        409,
        'Conflict',
        JSON.stringify({
          error: 'Mentor has already been activated and cannot be returned to draft',
        })
      )
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      headers: { cookie: 'admin_session=abc123' },
      body: { reason: 'Add a photo' },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(409)
    expect(res._getJSONData()).toEqual({
      error: 'Mentor has already been activated and cannot be returned to draft',
    })
  })

  it('returns 500 for unexpected errors', async () => {
    mockAdminReturnMentor.mockRejectedValue(new Error('connection refused'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      headers: { cookie: 'admin_session=abc123' },
      body: { reason: 'Add a photo' },
    })

    await returnHandler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
