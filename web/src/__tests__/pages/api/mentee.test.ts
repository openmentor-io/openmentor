import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockClient = {
  menteeRequestLogin: jest.fn(),
  menteeGetRequests: jest.fn(),
  menteeSubmitReview: jest.fn(),
}

jest.mock('@/lib/go-api-client', () => {
  const actual = jest.requireActual('@/lib/go-api-client')
  return { HttpError: actual.HttpError, getGoApiClient: () => mockClient }
})

import { HttpError } from '@/lib/go-api-client'
import requestLogin from '@/pages/api/mentee/auth/request-login'
import listRequests from '@/pages/api/mentee/requests'
import submitReview from '@/pages/api/mentee/reviews'

const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'
const COOKIE = 'mentee_session=test-session'

describe('api/mentee/auth/request-login', () => {
  beforeEach(() => jest.clearAllMocks())

  // M3: an address that never sent a request must be indistinguishable from
  // one that was mailed a link.
  it('answers identically whether or not the email is known', async () => {
    mockClient.menteeRequestLogin.mockResolvedValueOnce({ success: true })
    const known = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { email: 'known@example.com' },
    })
    await requestLogin(known.req, known.res)

    mockClient.menteeRequestLogin.mockRejectedValueOnce(new HttpError(404, 'Not Found', '{}'))
    const unknown = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { email: 'unknown@example.com' },
    })
    await requestLogin(unknown.req, unknown.res)

    expect(known.res.statusCode).toBe(200)
    expect(unknown.res.statusCode).toBe(200)
    expect(unknown.res._getJSONData()).toEqual(known.res._getJSONData())
  })

  it('rejects a missing email', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST', body: {} })
    await requestLogin(req, res)
    expect(res.statusCode).toBe(400)
    expect(mockClient.menteeRequestLogin).not.toHaveBeenCalled()
  })
})

describe('api/mentee/requests', () => {
  beforeEach(() => jest.clearAllMocks())

  it('refuses without a session cookie', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })
    await listRequests(req, res)
    expect(res.statusCode).toBe(401)
    expect(mockClient.menteeGetRequests).not.toHaveBeenCalled()
  })

  it('forwards the cookie and returns the list', async () => {
    mockClient.menteeGetRequests.mockResolvedValue({ requests: [] })
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
      headers: { cookie: COOKIE },
    })
    await listRequests(req, res)
    expect(mockClient.menteeGetRequests).toHaveBeenCalledWith(COOKIE)
    expect(res._getJSONData()).toEqual({ requests: [] })
  })

  it('passes an upstream 401 through so the page can send the mentee to log in', async () => {
    mockClient.menteeGetRequests.mockRejectedValue(
      new HttpError(401, 'Unauthorized', '{"error":"Unauthorized"}'),
    )
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'GET',
      headers: { cookie: COOKIE },
    })
    await listRequests(req, res)
    expect(res.statusCode).toBe(401)
  })
})

describe('api/mentee/reviews', () => {
  beforeEach(() => jest.clearAllMocks())

  const validBody = {
    requestId: REQUEST_ID,
    mentorReview: '  a genuinely useful hour  ',
    captchaToken: 'captcha',
  }

  // The request id is a legacy review capability while H4's dual-read window is
  // open, so it goes upstream in the body — the client method takes no id arg.
  it('forwards the review in the body with the session cookie', async () => {
    mockClient.menteeSubmitReview.mockResolvedValue({ success: true, reviewId: 'r1' })
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      body: validBody,
    })
    await submitReview(req, res)

    expect(res.statusCode).toBe(200)
    expect(mockClient.menteeSubmitReview).toHaveBeenCalledWith(COOKIE, {
      requestId: REQUEST_ID,
      mentorReview: 'a genuinely useful hour',
      platformReview: '',
      improvements: '',
      captchaToken: 'captcha',
    })
  })

  it.each([
    ['no session', {}, validBody, 401],
    ['a malformed request id', { cookie: COOKIE }, { ...validBody, requestId: '../x' }, 400],
    ['a too-short review', { cookie: COOKIE }, { ...validBody, mentorReview: 'meh' }, 400],
    ['no captcha', { cookie: COOKIE }, { ...validBody, captchaToken: '' }, 400],
  ])('refuses %s before calling the API', async (_name, headers, body, status) => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers,
      body,
    })
    await submitReview(req, res)
    expect(res.statusCode).toBe(status)
    expect(mockClient.menteeSubmitReview).not.toHaveBeenCalled()
  })

  it('relays an already-reviewed conflict', async () => {
    mockClient.menteeSubmitReview.mockRejectedValue(
      new HttpError(409, 'Conflict', '{"success":false,"error":"Review already submitted"}'),
    )
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      headers: { cookie: COOKIE },
      body: validBody,
    })
    await submitReview(req, res)
    expect(res.statusCode).toBe(409)
    expect((res._getJSONData() as { error: string }).error).toBe('Review already submitted')
  })
})
