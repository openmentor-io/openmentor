import { render, screen, fireEvent } from '@testing-library/react'
import { CookieConsentBanner } from '@/components'
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

describe('CookieConsentBanner', () => {
  beforeEach(() => {
    clearConsent()
  })

  it('renders the banner with Accept/Decline buttons and a privacy link when no choice stored', () => {
    render(<CookieConsentBanner />)

    expect(screen.getByText(/We use analytics cookies to improve OpenMentor/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Accept' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Decline' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /learn more/i })).toHaveAttribute('href', '/privacy')
  })

  it('does not render when a choice was already stored', () => {
    setConsent('accepted')

    render(<CookieConsentBanner />)

    expect(screen.queryByRole('dialog', { name: /cookie consent/i })).not.toBeInTheDocument()
  })

  it('persists the choice and hides the banner on Accept', () => {
    render(<CookieConsentBanner />)

    fireEvent.click(screen.getByRole('button', { name: 'Accept' }))

    expect(getConsent()).toBe('accepted')
    expect(document.cookie).toContain(`${CONSENT_STORAGE_KEY}=accepted`)
    expect(screen.queryByRole('dialog', { name: /cookie consent/i })).not.toBeInTheDocument()
  })

  it('persists the choice and hides the banner on Decline', () => {
    render(<CookieConsentBanner />)

    fireEvent.click(screen.getByRole('button', { name: 'Decline' }))

    expect(getConsent()).toBe('declined')
    expect(screen.queryByRole('dialog', { name: /cookie consent/i })).not.toBeInTheDocument()
  })

  it('does not initialize analytics when the user declines', () => {
    const initializeAnalytics = jest.fn()
    onAnalyticsConsentGranted(initializeAnalytics)

    render(<CookieConsentBanner />)
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }))

    expect(initializeAnalytics).not.toHaveBeenCalled()
  })

  it('initializes analytics without a reload when the user accepts', () => {
    const initializeAnalytics = jest.fn()
    onAnalyticsConsentGranted(initializeAnalytics)

    render(<CookieConsentBanner />)
    expect(initializeAnalytics).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Accept' }))

    expect(initializeAnalytics).toHaveBeenCalledTimes(1)
  })
})
