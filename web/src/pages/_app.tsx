import '../styles/brand-tokens.css'
import '../styles/globals.css'
import '@fortawesome/fontawesome-svg-core/styles.css'
import { useEffect } from 'react'
import { Inter } from 'next/font/google'
import TagManager from 'react-gtm-module'
import { CookieConsentBanner } from '@/components'
import { onAnalyticsConsentGranted } from '@/lib/consent'
import { initializeFaro } from '@/lib/faro'
import { initializePostHog } from '@/lib/posthog'
import type { AppProps } from 'next/app'

// Self-hosted via next/font (no external stylesheet request). The CSS
// variable is consumed by the Tailwind `font-sans` stack; a :root fallback
// for --font-inter lives in globals.css for content portaled outside the
// app wrapper.
const inter = Inter({
  subsets: ['latin', 'cyrillic'],
  display: 'swap',
  variable: '--font-inter',
})

// Initialize observability on client-side only (outside component to run once).
// Faro is operational error/performance monitoring (legitimate interest) and is
// not consent-gated; product analytics below are.
if (typeof window !== 'undefined') {
  initializeFaro()
}

let analyticsInitialized = false

// Product analytics (PostHog + GTM, which loads Mixpanel/GA) initialize only
// after the user accepts analytics cookies in the consent banner.
function initializeAnalytics(): void {
  if (analyticsInitialized) return
  analyticsInitialized = true

  initializePostHog()
  TagManager.initialize({ gtmId: 'GTM-NBGRPCZ' })
}

function MyApp({ Component, pageProps }: AppProps): JSX.Element {
  useEffect(() => {
    // Runs immediately if consent was already given, or retroactively (without
    // a page reload) when the user accepts the banner. Never runs on decline.
    return onAnalyticsConsentGranted(initializeAnalytics)
  }, [])

  return (
    <div className={`${inter.variable} font-sans`}>
      <Component {...pageProps} />
      <CookieConsentBanner />
    </div>
  )
}

export default MyApp
