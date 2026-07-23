/**
 * "Share your profile" card for the mentor profile edit page.
 *
 * Every approved mentor is a distribution channel: their profile link
 * unfurls into a branded OG card (photo, role, price) when posted on
 * LinkedIn/Slack/Telegram. This card makes the share a one-click action —
 * open LinkedIn's share dialog pre-filled with the profile URL, or copy
 * the link. Shown only while the profile is visible (an inactive profile
 * hides the contact button, so sharing it would disappoint).
 */

import { useEffect, useRef, useState } from 'react'
import analytics from '@/lib/analytics'

interface ShareProfileCardProps {
  slug: string
}

/** Profile URL with share attribution (see docs/analytics-utm.md). */
function shareUrl(slug: string): string {
  return `${window.location.origin}/mentor/${slug}?utm_source=mentor-share&utm_medium=social&utm_campaign=profile-share`
}

export default function ShareProfileCard({ slug }: ShareProfileCardProps): JSX.Element {
  const [copied, setCopied] = useState(false)
  const copyResetTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => () => clearTimeout(copyResetTimer.current), [])

  const handleLinkedInShare = (): void => {
    analytics.event(analytics.events.PROFILE_SHARE_CLICKED, { channel: 'linkedin' })
    const url = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(
      shareUrl(slug)
    )}`
    window.open(url, '_blank', 'noopener,noreferrer,width=600,height=600')
  }

  const handleCopy = async (): Promise<void> => {
    analytics.event(analytics.events.PROFILE_SHARE_CLICKED, { channel: 'copy-link' })
    try {
      await navigator.clipboard.writeText(shareUrl(slug))
      setCopied(true)
      clearTimeout(copyResetTimer.current)
      copyResetTimer.current = setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard unavailable (permissions/http) — fall back to a prompt.
      window.prompt('Copy your profile link:', shareUrl(slug))
    }
  }

  return (
    <div className="rounded-panel border-[1.5px] border-line bg-white p-5 sm:px-[26px] sm:py-[22px]">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between sm:gap-5">
        <div className="min-w-0">
          <span className="font-name text-base font-bold text-ink">Share your profile</span>
          <p className="my-0 mt-1 text-[13px] leading-normal text-ink-soft">
            Your link unfurls into a card with your photo and price — a personal &ldquo;I mentor
            here&rdquo; post reaches people the catalog never will.
          </p>
        </div>
        <div className="flex flex-none gap-2.5">
          <button
            type="button"
            onClick={handleLinkedInShare}
            className="button px-4 py-2.5 text-[13px]"
          >
            Share on LinkedIn
          </button>
          <button
            type="button"
            onClick={handleCopy}
            className="button-secondary px-4 py-2.5 text-[13px]"
            aria-live="polite"
          >
            {copied ? 'Copied ✓' : 'Copy link'}
          </button>
        </div>
      </div>
    </div>
  )
}
