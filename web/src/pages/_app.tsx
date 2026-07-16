import '../styles/brand-tokens.css'
import '../styles/design-tokens.css'
import '../styles/globals.css'
import '@fortawesome/fontawesome-svg-core/styles.css'
import { useEffect } from 'react'
import { useRouter } from 'next/router'
import { Archivo, IBM_Plex_Mono, Inter, Schibsted_Grotesk } from 'next/font/google'
import TagManager from 'react-gtm-module'
import { CookieConsentBanner } from '@/components'
import { onAnalyticsConsentGranted } from '@/lib/consent'
import { initializeFaro, trackRouteChange } from '@/lib/faro'
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

// Redesign display faces (component sheet type system): Archivo carries the
// CAPS headlines, Schibsted Grotesk the names/numbers, IBM Plex Mono the
// metadata rows. Inter stays the quiet body face.
const archivo = Archivo({
  subsets: ['latin'],
  weight: 'variable',
  display: 'swap',
  variable: '--font-archivo',
})

const schibsted = Schibsted_Grotesk({
  subsets: ['latin'],
  weight: 'variable',
  display: 'swap',
  variable: '--font-schibsted',
})

const plexMono = IBM_Plex_Mono({
  subsets: ['latin'],
  weight: ['400', '500'],
  display: 'swap',
  variable: '--font-plex-mono',
})

// Initialize observability on client-side only (outside component to run once).
// Faro is operational error/performance monitoring (legitimate interest) and is
// not consent-gated; product analytics below are.
if (typeof window !== 'undefined') {
  initializeFaro()
}

let analyticsInitialized = false

// Product analytics (PostHog + GTM, which loads GA) initialize only
// after the user accepts analytics cookies in the consent banner.
function initializeAnalytics(): void {
  if (analyticsInitialized) return
  analyticsInitialized = true

  initializePostHog()
  // Mixpanel tag must also be removed in the GTM console (GTM-NBGRPCZ) —
  // code cannot control container contents.
  TagManager.initialize({ gtmId: 'GTM-5GLW4WPS' })
}

function MyApp({ Component, pageProps }: AppProps): JSX.Element {
  const router = useRouter()

  useEffect(() => {
    // Runs immediately if consent was already given, or retroactively (without
    // a page reload) when the user accepts the banner. Never runs on decline.
    return onAnalyticsConsentGranted(initializeAnalytics)
  }, [])

  useEffect(() => {
    // Faro only instruments the initial hard load — track SPA navigations
    // (client-side route changes) as view updates + route_change events.
    // trackRouteChange is a no-op when Faro is not initialized.
    const handleRouteChangeComplete = (url: string): void => {
      trackRouteChange(url)
    }

    router.events.on('routeChangeComplete', handleRouteChangeComplete)
    return () => {
      router.events.off('routeChangeComplete', handleRouteChangeComplete)
    }
  }, [router.events])

  return (
    <div
      className={`${inter.variable} ${archivo.variable} ${schibsted.variable} ${plexMono.variable} flex min-h-screen flex-col font-sans`}
    >
      <Component {...pageProps} />
      <CookieConsentBanner />
    </div>
  )
}

export default MyApp
