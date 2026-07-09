import {
  CONSENT_STORAGE_KEY,
  getConsent,
  onAnalyticsConsentGranted,
  setConsent,
} from '@/lib/consent'

function clearConsent(): void {
  window.localStorage.clear()
  document.cookie = `${CONSENT_STORAGE_KEY}=; path=/; max-age=0`
}

describe('consent storage', () => {
  beforeEach(() => {
    clearConsent()
  })

  it('returns unset when no choice has been stored', () => {
    expect(getConsent()).toBe('unset')
  })

  it('persists an accepted choice in localStorage and a first-party cookie', () => {
    setConsent('accepted')

    expect(getConsent()).toBe('accepted')

    const stored = JSON.parse(window.localStorage.getItem(CONSENT_STORAGE_KEY) ?? '{}')
    expect(stored.status).toBe('accepted')
    expect(stored.expiresAt).toBeGreaterThan(Date.now())

    expect(document.cookie).toContain(`${CONSENT_STORAGE_KEY}=accepted`)
  })

  it('persists a declined choice', () => {
    setConsent('declined')

    expect(getConsent()).toBe('declined')
    expect(document.cookie).toContain(`${CONSENT_STORAGE_KEY}=declined`)
  })

  it('treats an expired localStorage entry as unset', () => {
    window.localStorage.setItem(
      CONSENT_STORAGE_KEY,
      JSON.stringify({ status: 'accepted', expiresAt: Date.now() - 1000 })
    )

    expect(getConsent()).toBe('unset')
    expect(window.localStorage.getItem(CONSENT_STORAGE_KEY)).toBeNull()
  })

  it('treats a corrupted localStorage entry as unset', () => {
    window.localStorage.setItem(CONSENT_STORAGE_KEY, 'not-json')

    expect(getConsent()).toBe('unset')
  })

  it('falls back to the cookie when localStorage has no entry', () => {
    document.cookie = `${CONSENT_STORAGE_KEY}=accepted; path=/`

    expect(getConsent()).toBe('accepted')
  })
})

describe('onAnalyticsConsentGranted', () => {
  beforeEach(() => {
    clearConsent()
  })

  it('runs the callback immediately when consent was already accepted', () => {
    setConsent('accepted')

    const callback = jest.fn()
    onAnalyticsConsentGranted(callback)

    expect(callback).toHaveBeenCalledTimes(1)
  })

  it('runs the callback retroactively when the user accepts later', () => {
    const callback = jest.fn()
    onAnalyticsConsentGranted(callback)

    expect(callback).not.toHaveBeenCalled()

    setConsent('accepted')

    expect(callback).toHaveBeenCalledTimes(1)
  })

  it('never runs the callback when consent is declined', () => {
    const callback = jest.fn()
    onAnalyticsConsentGranted(callback)

    setConsent('declined')

    expect(callback).not.toHaveBeenCalled()
  })

  it('does not run the callback after unsubscribe', () => {
    const callback = jest.fn()
    const unsubscribe = onAnalyticsConsentGranted(callback)

    unsubscribe()
    setConsent('accepted')

    expect(callback).not.toHaveBeenCalled()
  })
})
