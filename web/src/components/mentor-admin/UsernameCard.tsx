/**
 * "Your profile link" card for the mentor profile edit page.
 *
 * Shows the mentor's current username (the public part of their profile URL)
 * and lets them change it. Renaming is a BREAKING action — shared links and
 * cached OG cards point at the old URL — so it lives on its own endpoint
 * (separate from profile save), gets an explicit warning + confirmation, and
 * is rate-limited to once every 14 days. Old links redirect for 2 hops.
 */

import { useEffect, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faTimes } from '@fortawesome/free-solid-svg-icons'
import analytics from '@/lib/analytics'
import {
  useUsernameAvailability,
  isUsernameFormatValid,
  USERNAME_MAX_LENGTH,
} from '@/lib/username'
import type { UsernameStatusResponse } from '@/types'

interface UsernameCardProps {
  /** Current username, from the loaded mentor profile. */
  initialUsername: string
  /** Called with the new username after a successful change. */
  onChanged: (username: string) => void
}

/** Format a cooldown date as a friendly "Jul 25, 2026". */
function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

export default function UsernameCard({
  initialUsername,
  onChanged,
}: UsernameCardProps): JSX.Element {
  const [username, setUsername] = useState(initialUsername)
  const [status, setStatus] = useState<UsernameStatusResponse | null>(null)
  const [modalOpen, setModalOpen] = useState(false)

  useEffect(() => {
    let mounted = true
    fetch('/api/mentor/username')
      .then((res) => (res.ok ? res.json() : null))
      .then((data: UsernameStatusResponse | null) => {
        if (mounted && data) {
          setStatus(data)
          setUsername(data.username)
        }
      })
      .catch(() => {
        /* non-critical: fall back to the initial username */
      })
    return () => {
      mounted = false
    }
  }, [])

  const canChange = status?.canChange ?? true

  const handleChanged = (newUsername: string): void => {
    setUsername(newUsername)
    onChanged(newUsername)
    // Re-fetch status so the cooldown reflects the just-made change.
    fetch('/api/mentor/username')
      .then((res) => (res.ok ? res.json() : null))
      .then((data: UsernameStatusResponse | null) => {
        if (data) setStatus(data)
      })
      .catch(() => {})
    setModalOpen(false)
  }

  return (
    <div className="rounded-panel border-[1.5px] border-line bg-white p-5 sm:px-[26px] sm:py-[22px]">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between sm:gap-5">
        <div className="min-w-0">
          <span className="font-name text-base font-bold text-ink">Your profile link</span>
          <p className="my-0 mt-1 truncate text-[13px] leading-normal text-ink-soft">
            openmentor.io/mentor/<span className="font-mono text-ink">{username}</span>
          </p>
          {!canChange && status?.nextChangeAt && (
            <p className="my-0 mt-1 text-[13px] leading-normal text-ink-soft">
              You can change it again on {formatDate(status.nextChangeAt)}.
            </p>
          )}
        </div>
        <div className="flex flex-none gap-2.5">
          <button
            type="button"
            disabled={!canChange}
            onClick={() => setModalOpen(true)}
            className="button-secondary px-4 py-2.5 text-[13px] disabled:cursor-not-allowed disabled:opacity-50"
          >
            Change link…
          </button>
        </div>
      </div>

      <UsernameChangeModal
        isOpen={modalOpen}
        currentUsername={username}
        onClose={() => setModalOpen(false)}
        onChanged={handleChanged}
      />
    </div>
  )
}

interface UsernameChangeModalProps {
  isOpen: boolean
  currentUsername: string
  onClose: () => void
  onChanged: (username: string) => void
}

function UsernameChangeModal({
  isOpen,
  currentUsername,
  onClose,
  onChanged,
}: UsernameChangeModalProps): JSX.Element | null {
  const [value, setValue] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (isOpen) {
      setValue('')
      setConfirmed(false)
      setError(null)
    }
  }, [isOpen])

  useEffect(() => {
    const handleEscape = (e: KeyboardEvent): void => {
      if (e.key === 'Escape' && isOpen && !submitting) onClose()
    }
    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [isOpen, submitting, onClose])

  const availability = useUsernameAvailability(value, '/api/mentor/username')
  const formatValid = isUsernameFormatValid(value)
  const isAvailable = availability.status === 'available'
  const canSubmit = formatValid && isAvailable && confirmed && !submitting

  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/api/mentor/username', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: value }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setError(data.error || 'Failed to change your profile link')
        return
      }
      analytics.event(analytics.events.MENTOR_USERNAME_CHANGED)
      onChanged(data.username ?? value)
    } catch {
      setError('Failed to change your profile link')
    } finally {
      setSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 overflow-y-auto"
      aria-labelledby="username-modal-title"
      role="dialog"
      aria-modal="true"
    >
      <div
        className="fixed inset-0 bg-ink/40 transition-opacity"
        onClick={(e) => {
          if (e.target === e.currentTarget && !submitting) onClose()
        }}
      />

      <div className="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
        <div className="relative w-full transform overflow-hidden rounded-panel bg-white text-left shadow-dropdown transition-all sm:my-8 sm:max-w-lg">
          <div className="flex items-center justify-between border-b border-line px-6 py-4">
            <h3
              id="username-modal-title"
              className="font-display text-sm font-extrabold uppercase tracking-[0.03em] text-ink"
            >
              Change your profile link
            </h3>
            <button
              onClick={onClose}
              disabled={submitting}
              className="text-ink-soft transition-colors duration-120 hover:text-ink disabled:opacity-50"
              aria-label="Close"
            >
              <FontAwesomeIcon icon={faTimes} />
            </button>
          </div>

          <form onSubmit={handleSubmit}>
            <div className="space-y-4 px-6 py-4">
              {/* Breaking-change warning */}
              <div className="rounded-field border border-danger/40 bg-danger/[0.06] p-3">
                <p className="my-0 text-[13px] leading-relaxed text-ink">
                  Changing your link is a big deal. Anywhere you&apos;ve shared{' '}
                  <span className="font-mono font-semibold">/mentor/{currentUsername}</span> — posts,
                  your CV, DMs — will redirect for now, but that old link{' '}
                  <span className="font-semibold">stops working after two more changes</span>. Cards
                  already posted on social may keep showing the old link from cache. You can only
                  change your link once every 14 days.
                </p>
              </div>

              <div>
                <label
                  htmlFor="new-username"
                  className="mb-1.5 block text-[13px] font-semibold text-ink"
                >
                  New link
                </label>
                <div className="flex items-center overflow-hidden rounded-field border-[1.5px] border-line bg-white focus-within:border-brand-cobalt">
                  <span className="select-none whitespace-nowrap py-2.5 pl-3.5 pr-0 font-mono text-sm text-ink-soft">
                    openmentor.io/mentor/
                  </span>
                  <input
                    id="new-username"
                    value={value}
                    autoFocus
                    autoComplete="off"
                    autoCapitalize="none"
                    spellCheck={false}
                    maxLength={USERNAME_MAX_LENGTH}
                    disabled={submitting}
                    onChange={(e) =>
                      setValue(
                        e.target.value
                          .toLowerCase()
                          .replace(/[^a-z0-9-]+/g, '-')
                          .replace(/-{2,}/g, '-')
                      )
                    }
                    className="w-full min-w-0 border-0 bg-transparent py-2.5 pl-0 pr-3.5 font-mono text-sm text-ink outline-none"
                    placeholder="your-name"
                  />
                </div>

                <div
                  className="mt-2 min-h-[1.25rem] text-[13px]"
                  role="status"
                  aria-live="polite"
                >
                  {availability.status === 'checking' && (
                    <span className="text-ink-soft">
                      <FontAwesomeIcon icon={faCircleNotch} spin className="mr-1.5" />
                      Checking availability…
                    </span>
                  )}
                  {availability.status === 'available' && (
                    <span className="font-medium text-mint-ink">✓ Available</span>
                  )}
                  {availability.status === 'unavailable' && value !== '' && (
                    <span className="font-medium text-danger">
                      {availability.reason === 'taken'
                        ? 'That link is already taken — try another.'
                        : availability.reason === 'reserved'
                          ? 'That word is reserved — please pick another.'
                          : 'Use 3–40 letters, numbers and hyphens.'}
                    </span>
                  )}
                </div>
              </div>

              <label className="flex cursor-pointer items-start gap-2.5 text-[13px] text-ink">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={submitting}
                  onChange={(e) => setConfirmed(e.target.checked)}
                  className="mt-0.5 h-4 w-4 flex-none accent-brand-cobalt"
                />
                <span>
                  I understand my old link will eventually stop working, and I want to change it.
                </span>
              </label>

              {error && (
                <p className="my-0 animate-shake text-sm font-medium text-danger" role="alert">
                  {error}
                </p>
              )}
            </div>

            <div className="flex flex-col-reverse gap-3 border-t border-line bg-surface px-6 py-4 sm:flex-row sm:justify-end">
              <button
                type="button"
                onClick={onClose}
                disabled={submitting}
                className="button-secondary w-full text-sm sm:w-auto"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!canSubmit}
                className="button w-full text-sm disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              >
                {submitting ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                    Changing…
                  </>
                ) : (
                  'Change my link'
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
