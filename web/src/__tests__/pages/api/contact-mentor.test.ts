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

const mockContactMentor = jest.fn()

jest.mock('@/lib/go-api-client', () => {
  // Keep the real HttpError so sendUpstreamError's instanceof check works.
  const actual = jest.requireActual('@/lib/go-api-client')
  return {
    HttpError: actual.HttpError,
    getGoApiClient: () => ({
      contactMentor: mockContactMentor,
    }),
  }
})

// Import handler after mocks are set up
import handler from '@/pages/api/contact-mentor'

describe('api/contact-mentor', () => {
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

  it('forwards contact request to Go API and returns response', async () => {
    const contactData = {
      mentorId: 'rec123',
      name: 'John Doe',
      email: 'john@example.com',
      message: 'Hello mentor!',
      captchaToken: 'valid-token',
    }

    const mockResponse = { success: true, message: 'Email sent' }
    mockContactMentor.mockResolvedValue(mockResponse)

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: contactData,
    })

    await handler(req, res)

    expect(mockContactMentor).toHaveBeenCalledWith(contactData)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual(mockResponse)
  })

  it('returns 500 when Go API throws an error', async () => {
    mockContactMentor.mockRejectedValue(new Error('API connection failed'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { mentorId: 'rec123' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })

  // The contact form is the primary conversion path: a captcha rejection or a
  // validation error has to reach the mentee as itself, not as "something went
  // wrong" — which is all a blanket 500 can produce.
  it.each([
    [400, 'Bad Request', { error: 'Captcha verification failed' }],
    [429, 'Too Many Requests', { error: 'Too many requests, please try again later' }],
  ])('forwards an upstream %i to the client', async (status, statusText, body) => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockContactMentor.mockRejectedValue(new HttpError(status, statusText, JSON.stringify(body)))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { mentorId: 'rec123' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(status)
    expect(res._getJSONData()).toEqual(body)
  })

  it('hides an upstream 5xx behind a generic 500', async () => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockContactMentor.mockRejectedValue(
      new HttpError(503, 'Service Unavailable', 'dial tcp 127.0.0.1:8080: connect: refused')
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { mentorId: 'rec123' },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
