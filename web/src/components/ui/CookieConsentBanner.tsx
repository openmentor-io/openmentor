import Link from 'next/link'
import { useEffect, useState } from 'react'
import { getConsent, setConsent } from '@/lib/consent'
import type { ConsentChoice } from '@/lib/consent'

/**
 * Lightweight self-built cookie-consent banner (fixed bottom bar).
 * Shown until the user makes a choice; the choice is stored for 12 months
 * (localStorage + first-party cookie) via `@/lib/consent`.
 */
export default function CookieConsentBanner(): JSX.Element | null {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    // Consent is read client-side only, so decide visibility after mount
    setVisible(getConsent() === 'unset')
  }, [])

  if (!visible) return null

  const choose = (choice: ConsentChoice): void => {
    setConsent(choice)
    setVisible(false)
  }

  return (
    <div
      role="dialog"
      aria-label="Cookie consent"
      className="fixed inset-x-0 bottom-0 z-50 bg-gray-900 px-4 py-4 text-sm text-white"
      data-section="cookie-consent"
    >
      <div className="mx-auto flex max-w-4xl flex-col items-center gap-3 sm:flex-row sm:justify-between">
        <p className="text-center sm:text-left">
          We use analytics cookies to improve OpenMentor. Essential cookies are always on.{' '}
          <Link href="/privacy" className="underline hover:text-gray-300">
            Learn more
          </Link>
        </p>
        <div className="flex shrink-0 gap-2">
          <button type="button" className="button" onClick={() => choose('accepted')}>
            Accept
          </button>
          <button
            type="button"
            className="rounded border border-gray-500 px-4 py-2 hover:bg-gray-700"
            onClick={() => choose('declined')}
          >
            Decline
          </button>
        </div>
      </div>
    </div>
  )
}
