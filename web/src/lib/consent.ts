/**
 * Cookie/analytics consent storage.
 *
 * The user's choice is stored client-side in localStorage (with an explicit
 * expiry) and mirrored in a first-party cookie, both valid for 12 months.
 * Analytics (PostHog, GTM/Mixpanel/GA) must only initialize after the user
 * accepts — see `onAnalyticsConsentGranted` and its usage in `_app.tsx`.
 */

export type ConsentChoice = 'accepted' | 'declined'
export type ConsentStatus = ConsentChoice | 'unset'

export const CONSENT_STORAGE_KEY = 'om_consent'
export const CONSENT_CHANGE_EVENT = 'om:consent-change'

/** 12 months */
const CONSENT_TTL_MS = 365 * 24 * 60 * 60 * 1000

interface StoredConsent {
  status: ConsentChoice
  expiresAt: number
}

function isConsentChoice(value: unknown): value is ConsentChoice {
  return value === 'accepted' || value === 'declined'
}

function readFromLocalStorage(): ConsentStatus {
  try {
    const raw = window.localStorage.getItem(CONSENT_STORAGE_KEY)
    if (!raw) return 'unset'

    const parsed = JSON.parse(raw) as Partial<StoredConsent>
    if (!isConsentChoice(parsed.status) || typeof parsed.expiresAt !== 'number') {
      return 'unset'
    }

    if (Date.now() >= parsed.expiresAt) {
      window.localStorage.removeItem(CONSENT_STORAGE_KEY)
      return 'unset'
    }

    return parsed.status
  } catch {
    // localStorage unavailable or corrupted value — fall back to the cookie
    return 'unset'
  }
}

function readFromCookie(): ConsentStatus {
  const match = document.cookie.match(
    new RegExp(`(?:^|;\\s*)${CONSENT_STORAGE_KEY}=(accepted|declined)(?:;|$)`)
  )
  return match && isConsentChoice(match[1]) ? match[1] : 'unset'
}

/**
 * Returns the user's stored consent choice, or 'unset' when no valid,
 * unexpired choice exists (in which case the banner should be shown).
 */
export function getConsent(): ConsentStatus {
  if (typeof window === 'undefined') return 'unset'

  const fromStorage = readFromLocalStorage()
  if (fromStorage !== 'unset') return fromStorage

  return readFromCookie()
}

/**
 * Persists the user's choice (12-month expiry) and notifies listeners so
 * analytics can initialize retroactively without a page reload.
 */
export function setConsent(choice: ConsentChoice): void {
  if (typeof window === 'undefined') return

  const stored: StoredConsent = { status: choice, expiresAt: Date.now() + CONSENT_TTL_MS }
  try {
    window.localStorage.setItem(CONSENT_STORAGE_KEY, JSON.stringify(stored))
  } catch {
    // localStorage unavailable — the cookie below still records the choice
  }

  document.cookie = `${CONSENT_STORAGE_KEY}=${choice}; path=/; max-age=${Math.floor(
    CONSENT_TTL_MS / 1000
  )}; SameSite=Lax`

  window.dispatchEvent(new CustomEvent<ConsentChoice>(CONSENT_CHANGE_EVENT, { detail: choice }))
}

/**
 * Runs `callback` once analytics consent is granted: immediately if the user
 * already accepted, or as soon as they accept the banner. Never runs the
 * callback if consent is declined. Returns an unsubscribe function.
 */
export function onAnalyticsConsentGranted(callback: () => void): () => void {
  if (typeof window === 'undefined') return () => {}

  if (getConsent() === 'accepted') {
    callback()
    return () => {}
  }

  const handler = (event: Event): void => {
    const detail = (event as CustomEvent<ConsentChoice>).detail
    if (detail === 'accepted') {
      window.removeEventListener(CONSENT_CHANGE_EVENT, handler)
      callback()
    }
  }

  window.addEventListener(CONSENT_CHANGE_EVENT, handler)
  return () => window.removeEventListener(CONSENT_CHANGE_EVENT, handler)
}
