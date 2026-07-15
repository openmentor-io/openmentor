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

const mockScheduleMigration = jest.fn()

jest.mock('@/lib/go-api-client', () => ({
  getGoApiClient: () => ({
    scheduleMigration: mockScheduleMigration,
  }),
}))

// Import handler after mocks are set up
import handler from '@/pages/api/schedule-migration'

describe('api/schedule-migration', () => {
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

  it('forwards the intent to the Go API and returns its response', async () => {
    const body = { slug: 'ivan-petrov-42', captchaToken: 'valid-token' }
    mockScheduleMigration.mockResolvedValue({ success: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body,
    })

    await handler(req, res)

    expect(mockScheduleMigration).toHaveBeenCalledWith(body)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true })
  })

  it('passes through alreadyScheduled responses', async () => {
    mockScheduleMigration.mockResolvedValue({ success: true, alreadyScheduled: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { slug: 'ivan-petrov-42', captchaToken: 'valid-token' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true, alreadyScheduled: true })
  })

  it('returns 500 when the Go API call fails', async () => {
    mockScheduleMigration.mockRejectedValue(new Error('go api down'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { slug: 'ivan-petrov-42', captchaToken: 'valid-token' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
