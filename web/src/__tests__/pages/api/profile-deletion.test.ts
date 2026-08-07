import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockMentorDeleteProfile = jest.fn()
const mockAdminDeleteMentor = jest.fn()
const mockAdminRestoreMentor = jest.fn()

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
      mentorDeleteProfile: mockMentorDeleteProfile,
      adminDeleteMentor: mockAdminDeleteMentor,
      adminRestoreMentor: mockAdminRestoreMentor,
    }),
  }
})

import mentorDeleteHandler from '@/pages/api/mentor/profile/delete'
import adminDeleteHandler from '@/pages/api/admin/mentors/[id]/delete'
import adminRestoreHandler from '@/pages/api/admin/mentors/[id]/restore'
import { HttpError } from '@/lib/go-api-client'

const COOKIE = 'mentor_session=abc'

describe('api/mentor/profile/delete', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })
    await mentorDeleteHandler(req, res)
    expect(res.statusCode).toBe(405)
  })

  it('returns 401 without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { username: 'anna-petrova-42' },
    })
    await mentorDeleteHandler(req, res)
    expect(res.statusCode).toBe(401)
    expect(mockMentorDeleteProfile).not.toHaveBeenCalled()
  })

  it('rejects a missing username confirmation before calling the API', async () => {
    for (const body of [{}, { username: '' }, { username: '   ' }, { username: 42 }]) {
      const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
        method: 'POST',
        headers: { cookie: COOKIE },
        body,
      })
      await mentorDeleteHandler(req, res)
      expect(res.statusCode).toBe(400)
    }
    expect(mockMentorDeleteProfile).not.toHaveBeenCalled()
  })

  it('forwards the confirmation to the Go API', async () => {
    mockMentorDeleteProfile.mockResolvedValue({ success: true })
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      body: { username: 'anna-petrova-42' },
    })

    await mentorDeleteHandler(req, res)

    expect(res.statusCode).toBe(200)
    expect(mockMentorDeleteProfile).toHaveBeenCalledWith(COOKIE, { username: 'anna-petrova-42' })
  })

  // A username mismatch is something the mentor can fix, so the upstream
  // status has to survive the proxy rather than collapse into a 500.
  it('passes the upstream rejection through', async () => {
    mockMentorDeleteProfile.mockRejectedValue(
      new HttpError(400, 'Bad Request', '{"error":"The username you entered does not match"}')
    )
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      body: { username: 'wrong' },
    })

    await mentorDeleteHandler(req, res)

    expect(res.statusCode).toBe(400)
  })
})

describe('api/admin/mentors/[id]/delete', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 401 without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
      body: { username: 'anna-petrova-42' },
    })
    await adminDeleteHandler(req, res)
    expect(res.statusCode).toBe(401)
    expect(mockAdminDeleteMentor).not.toHaveBeenCalled()
  })

  it('requires a username confirmation', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      query: { id: 'mentor-1' },
      body: {},
    })
    await adminDeleteHandler(req, res)
    expect(res.statusCode).toBe(400)
    expect(mockAdminDeleteMentor).not.toHaveBeenCalled()
  })

  it('forwards the mentor id and confirmation', async () => {
    mockAdminDeleteMentor.mockResolvedValue({ mentorId: 'mentor-1', deletedAt: '2026-08-06' })
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      query: { id: 'mentor-1' },
      body: { username: 'anna-petrova-42' },
    })

    await adminDeleteHandler(req, res)

    expect(res.statusCode).toBe(200)
    expect(mockAdminDeleteMentor).toHaveBeenCalledWith(COOKIE, 'mentor-1', {
      username: 'anna-petrova-42',
    })
  })

  // A moderator reaching this endpoint is a 403 from the Go API, which owns
  // the role rule — the proxy must not soften it.
  it('passes a forbidden response through', async () => {
    mockAdminDeleteMentor.mockRejectedValue(
      new HttpError(403, 'Forbidden', '{"error":"Access denied"}')
    )
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      query: { id: 'mentor-1' },
      body: { username: 'anna-petrova-42' },
    })

    await adminDeleteHandler(req, res)

    expect(res.statusCode).toBe(403)
  })
})

describe('api/admin/mentors/[id]/restore', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 401 without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      query: { id: 'mentor-1' },
    })
    await adminRestoreHandler(req, res)
    expect(res.statusCode).toBe(401)
    expect(mockAdminRestoreMentor).not.toHaveBeenCalled()
  })

  it('restores without requiring a body, and the profile comes back inactive', async () => {
    const restored = { mentorId: 'mentor-1', status: 'inactive', deletedAt: null }
    mockAdminRestoreMentor.mockResolvedValue(restored)
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      query: { id: 'mentor-1' },
    })

    await adminRestoreHandler(req, res)

    expect(res.statusCode).toBe(200)
    expect(mockAdminRestoreMentor).toHaveBeenCalledWith(COOKIE, 'mentor-1')
    expect(res._getJSONData()).toEqual({ mentor: restored })
  })

  it('passes a conflict on an already-live profile through', async () => {
    mockAdminRestoreMentor.mockRejectedValue(
      new HttpError(409, 'Conflict', '{"error":"This profile is not deleted"}')
    )
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      query: { id: 'mentor-1' },
    })

    await adminRestoreHandler(req, res)

    expect(res.statusCode).toBe(409)
  })
})
