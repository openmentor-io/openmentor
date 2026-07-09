import type posthog from 'posthog-js'

// Mock the posthog module so analytics.ts gets a controllable client
const mockPostHogClient: { current: Partial<typeof posthog> | null } = { current: null }
jest.mock('@/lib/posthog', () => ({
  getPostHogClient: () => mockPostHogClient.current,
}))

describe('analytics', () => {
  beforeEach(() => {
    jest.useFakeTimers()
    jest.resetModules()
    mockPostHogClient.current = null
    delete process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER
  })

  afterEach(() => {
    jest.useRealTimers()
    mockPostHogClient.current = null
  })

  it('sanitizes properties and adds common metadata', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'posthog'

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.MENTOR_REGISTRATION_SUBMITTED, {
      email: 'private@openmentor.io',
      name: 'Private Mentor',
      tags_count: 3,
    })

    expect(capture).toHaveBeenCalledTimes(1)
    const [, payload] = capture.mock.calls[0]
    expect(payload).toMatchObject({
      tags_count: 3,
      source_system: 'frontend',
      event_version: 'v1',
    })
    expect(payload.email).toBeUndefined()
    expect(payload.name).toBeUndefined()
  })

  it('keeps safe aggregate keys that include blocked fragments', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'posthog'

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.MENTEE_CONTACT_SUBMITTED, {
      has_telegram_username: true,
      review_id: 'rev_123',
      mentor_review: 'raw review text',
    })

    expect(capture).toHaveBeenCalledTimes(1)
    const [, payload] = capture.mock.calls[0]
    expect(payload).toMatchObject({
      has_telegram_username: true,
      review_id: 'rev_123',
    })
    expect(payload.mentor_review).toBeUndefined()
  })

  it('queues posthog events until posthog is available', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'posthog'

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.HOME_PAGE_VIEWED, {
      foo: 'bar',
      email: 'private@openmentor.io',
    })

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    jest.runOnlyPendingTimers()

    expect(capture).toHaveBeenCalledTimes(1)
    const [eventName, properties] = capture.mock.calls[0]
    expect(eventName).toBe(analyticsEvents.HOME_PAGE_VIEWED)
    expect(properties).toMatchObject({
      foo: 'bar',
      source_system: 'frontend',
      event_version: 'v1',
    })
    expect(properties.email).toBeUndefined()
  })

  it('queues identify and reset commands until posthog is available', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'posthog'

    const { default: analytics } = await import('@/lib/analytics')
    analytics.identify('mentor:123', {
      role: 'mentor',
      email: 'private@openmentor.io',
    })
    analytics.reset()

    const identify = jest.fn()
    const reset = jest.fn()
    mockPostHogClient.current = {
      capture: jest.fn(),
      identify,
      reset,
    } as unknown as typeof posthog

    jest.runOnlyPendingTimers()

    expect(identify).toHaveBeenCalledWith('mentor:123', { role: 'mentor' })
    expect(reset).toHaveBeenCalledTimes(1)
  })

  it('stops retry loop and drops queued commands when posthog never loads', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'posthog'

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.HOME_PAGE_VIEWED, { foo: 'bar' })

    for (let i = 0; i < 30; i += 1) {
      jest.runOnlyPendingTimers()
    }

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    jest.runOnlyPendingTimers()
    expect(capture).toHaveBeenCalledTimes(0)
  })

  it('defaults to posthog when the provider env var is unset', async () => {
    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.HOME_PAGE_VIEWED, { foo: 'bar' })

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    jest.runOnlyPendingTimers()

    expect(capture).toHaveBeenCalledTimes(1)
    const [eventName, properties] = capture.mock.calls[0]
    expect(eventName).toBe(analyticsEvents.HOME_PAGE_VIEWED)
    expect(properties).toMatchObject({
      foo: 'bar',
      source_system: 'frontend',
      event_version: 'v1',
    })
  })

  it('treats unknown provider values as posthog', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'legacy-provider'

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.HOME_PAGE_VIEWED, { foo: 'bar' })

    expect(capture).toHaveBeenCalledTimes(1)
  })

  it('sends nothing when the provider is none', async () => {
    process.env.NEXT_PUBLIC_ANALYTICS_PROVIDER = 'none'

    const capture = jest.fn()
    mockPostHogClient.current = {
      capture,
      identify: jest.fn(),
      reset: jest.fn(),
    } as unknown as typeof posthog

    const { default: analytics, analyticsEvents } = await import('@/lib/analytics')
    analytics.event(analyticsEvents.HOME_PAGE_VIEWED, { foo: 'bar' })

    for (let i = 0; i < 30; i += 1) {
      jest.runOnlyPendingTimers()
    }

    expect(capture).toHaveBeenCalledTimes(0)
  })
})
