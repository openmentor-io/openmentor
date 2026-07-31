/**
 * Meta row item — the label/value pairs under a request's message
 * ("Level", "Email", "Contact", "Received") on the mentor and admin
 * request pages.
 *
 * Values here are mentee-supplied free text (a contact line is up to 100
 * characters), so the cell must stay one line tall no matter what is in
 * it — a wrapping "how to reach me" would push the rest of the page down.
 * Clipping it with an ellipsis, however, hid exactly the part the mentor
 * needs: the handle at the end of "email or telegram: @…". So the value
 * scrolls horizontally instead of truncating, stays selectable, and
 * `copyable` fields get a one-click copy button.
 */

import { useEffect, useRef, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCheck, faCopy } from '@fortawesome/free-solid-svg-icons'

interface MetaItemProps {
  label: string
  value: string
  /** Wraps the value in a link — `mailto:` for emails. */
  href?: string
  /** Adds a copy button; use for values the reader has to act on. */
  copyable?: boolean
}

export default function MetaItem({ label, value, href, copyable }: MetaItemProps): JSX.Element {
  const [copied, setCopied] = useState(false)
  const copyResetTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => () => clearTimeout(copyResetTimer.current), [])

  const handleCopy = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      clearTimeout(copyResetTimer.current)
      copyResetTimer.current = setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard unavailable (permissions / insecure context) — fall back
      // to a prompt the reader can copy out of by hand.
      window.prompt(`Copy ${label.toLowerCase()}:`, value)
    }
  }

  return (
    <div className="min-w-0">
      <div className="text-xs text-ink-soft">{label}</div>
      <div className="flex items-start gap-1.5">
        <div className="scroll-line min-w-0 flex-1 text-[13px] font-semibold" title={value}>
          {href ? (
            <a
              href={href}
              className="text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              {value}
            </a>
          ) : (
            <span className="text-ink">{value}</span>
          )}
        </div>
        {copyable && (
          <button
            type="button"
            onClick={handleCopy}
            title={copied ? 'Copied' : `Copy ${label.toLowerCase()}`}
            aria-label={copied ? `${label} copied` : `Copy ${label.toLowerCase()}`}
            className="-mt-0.5 flex-none rounded-field p-1 text-[11px] text-ink-soft transition-colors duration-120 hover:bg-surface hover:text-ink"
          >
            <FontAwesomeIcon icon={copied ? faCheck : faCopy} />
          </button>
        )}
      </div>
      <span aria-live="polite" className="sr-only">
        {copied ? `${label} copied to clipboard` : ''}
      </span>
    </div>
  )
}
