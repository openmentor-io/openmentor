/**
 * SENTINEL test for P14: the contact and review flows must not be able to hand a
 * capability to PostHog, whatever they call the property.
 */

const capture = jest.fn()
const identify = jest.fn()

jest.mock('@/lib/posthog', () => ({
  getPostHogClient: () => ({ capture, identify, reset: jest.fn() }),
}))

import analytics from '@/lib/analytics'

const REVIEW_CAPABILITY = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'
const LOGIN_TOKEN = 'eyJhbGciOiJIUzI1NiJ9.c2VudGluZWw.s1gn4tur3'

describe('analytics property blocklist', () => {
  beforeEach(() => {
    capture.mockClear()
  })

  it('drops capability-bearing properties in every spelling', () => {
    analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
      mentor_id: 'mentor-1',
      request_id: REVIEW_CAPABILITY,
      requestId: REVIEW_CAPABILITY,
      'REQUEST-ID': REVIEW_CAPABILITY,
      client_request_id: REVIEW_CAPABILITY,
      captchaToken: LOGIN_TOKEN,
      login_token: LOGIN_TOKEN,
      confirm_url: `https://openmentor.io/mentor/confirm?token=${LOGIN_TOKEN}`,
      outcome: 'success',
    })

    expect(capture).toHaveBeenCalledTimes(1)
    const [event, properties] = capture.mock.calls[0]
    const rendered = JSON.stringify(properties)

    expect(event).toBe('mentee_contact_submitted')
    expect(rendered).not.toContain(REVIEW_CAPABILITY)
    expect(rendered).not.toContain(LOGIN_TOKEN)
    // The dimensions that are not capabilities still have to arrive.
    expect(properties).toMatchObject({ mentor_id: 'mentor-1', outcome: 'success' })
  })

  it('keeps derived, non-secret metrics', () => {
    analytics.event(analytics.events.REVIEW_PAGE_VIEWED, {
      has_request_id: true,
      captcha_token_length: LOGIN_TOKEN.length,
    })

    const [, properties] = capture.mock.calls[0]
    expect(properties).toMatchObject({
      has_request_id: true,
      captcha_token_length: LOGIN_TOKEN.length,
    })
  })
})
