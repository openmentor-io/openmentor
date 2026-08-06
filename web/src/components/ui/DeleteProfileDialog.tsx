/**
 * Profile deletion confirm dialog (D70).
 *
 * Shared by the two places a profile can be deleted — the mentor's own edit
 * page and the admin moderation detail page — because the confirmation is the
 * safety mechanism and two copies of it would drift. Only the surrounding
 * wording differs, via props.
 *
 * The design does three things, in order of how much they matter:
 *
 *  1. A large warning mark, immediately, so the dialog cannot be mistaken for
 *     any other confirm sheet on the site.
 *  2. States plainly what is lost and that it does not come back.
 *  3. Gates the confirm button on retyping the profile's username. Typing is
 *     the point: it makes deletion an action you take deliberately rather than
 *     one you can click through, and it names the profile out loud on a page
 *     where the next profile is one click away.
 */

import { useEffect, useRef, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faTimes, faTriangleExclamation } from '@fortawesome/free-solid-svg-icons'

interface DeleteProfileDialogProps {
  isOpen: boolean
  onClose: () => void
  /** Runs the delete. Rejecting shows the error inside the dialog. */
  onConfirm: () => Promise<void>
  /** The username the confirmation input must match. */
  username: string
  /** Dialog heading. */
  title: string
  /** What exactly is being deleted — differs for self vs admin deletion. */
  children: React.ReactNode
  confirmLabel?: string
}

/**
 * Match the server's rule exactly (services.usernameConfirmationMatches):
 * trimmed and case-insensitive. A client that is stricter than the server
 * disables a button the server would have accepted; a client that is looser
 * sends a request that fails for a reason the mentor already fixed.
 */
export function confirmationMatches(typed: string, username: string): boolean {
  return typed.trim().toLowerCase() === username.trim().toLowerCase()
}

export default function DeleteProfileDialog({
  isOpen,
  onClose,
  onConfirm,
  username,
  title,
  children,
  confirmLabel = 'Delete profile permanently',
}: DeleteProfileDialogProps): JSX.Element | null {
  const [typed, setTyped] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Reset on every open: a dialog that reopens still holding a matching
  // username would be one click from deleting, which is the thing this
  // component exists to prevent.
  useEffect(() => {
    if (isOpen) {
      setTyped('')
      setError(null)
      setIsSubmitting(false)
      const timer = setTimeout(() => inputRef.current?.focus(), 100)
      return () => clearTimeout(timer)
    }
    return undefined
  }, [isOpen])

  useEffect(() => {
    const handleEscape = (e: KeyboardEvent): void => {
      if (e.key === 'Escape' && isOpen && !isSubmitting) {
        onClose()
      }
    }
    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [isOpen, isSubmitting, onClose])

  const canConfirm = confirmationMatches(typed, username) && !isSubmitting

  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!canConfirm) return

    setIsSubmitting(true)
    setError(null)
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete the profile')
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 overflow-y-auto"
      aria-labelledby="delete-profile-title"
      role="dialog"
      aria-modal="true"
    >
      <div
        className="fixed inset-0 bg-ink/50 transition-opacity"
        onClick={(e) => {
          // Backdrop dismissal is fine here — closing is the safe direction,
          // and the typed confirmation is discarded on the next open.
          if (e.target === e.currentTarget && !isSubmitting) onClose()
        }}
      />

      <div className="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
        <div className="relative w-full transform overflow-hidden rounded-panel bg-white text-left shadow-dropdown transition-all sm:my-8 sm:max-w-lg">
          <div className="flex items-start justify-between border-b border-line px-6 py-4">
            <h3
              id="delete-profile-title"
              className="font-display text-sm font-extrabold uppercase tracking-[0.03em] text-danger"
            >
              {title}
            </h3>
            <button
              type="button"
              onClick={onClose}
              disabled={isSubmitting}
              className="text-ink-soft transition-colors duration-120 hover:text-ink disabled:opacity-50"
              aria-label="Close"
            >
              <FontAwesomeIcon icon={faTimes} />
            </button>
          </div>

          <form onSubmit={handleSubmit}>
            <div className="px-6 py-6">
              {/* The warning mark. Deliberately oversized and centred: this is
                  the one confirm sheet on the site whose action cannot be
                  undone, and it should not look like the others. */}
              <div className="flex flex-col items-center text-center">
                <div className="flex h-20 w-20 items-center justify-center rounded-full bg-danger/10">
                  <FontAwesomeIcon
                    icon={faTriangleExclamation}
                    className="text-4xl text-danger"
                    aria-hidden="true"
                  />
                </div>
                <p className="my-0 mt-4 font-name text-lg font-bold tracking-[-0.015em] text-ink">
                  This cannot be undone
                </p>
                <div className="mt-2 text-sm leading-relaxed text-ink-soft">{children}</div>
              </div>

              <div className="mt-6">
                <label
                  htmlFor="delete-confirm-username"
                  className="mb-1.5 block text-[13px] font-semibold text-ink"
                >
                  Type <span className="font-mono font-bold text-danger">{username}</span> to
                  confirm
                </label>
                <input
                  id="delete-confirm-username"
                  ref={inputRef}
                  type="text"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  disabled={isSubmitting}
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  placeholder={username}
                  className="field font-mono"
                  aria-describedby="delete-confirm-help"
                />
                <p id="delete-confirm-help" className="my-0 mt-1.5 text-[13px] text-ink-mute">
                  {canConfirm
                    ? 'Username matches — the delete button is now enabled.'
                    : 'The delete button stays disabled until this matches.'}
                </p>
              </div>

              {error && (
                <p className="my-0 mt-4 animate-shake text-sm font-medium text-danger" role="alert">
                  {error}
                </p>
              )}
            </div>

            <div className="flex flex-col-reverse gap-3 border-t border-line bg-surface px-6 py-4 sm:flex-row sm:justify-end">
              <button
                type="button"
                onClick={onClose}
                disabled={isSubmitting}
                className="button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!canConfirm}
                className="button-destructive disabled:cursor-not-allowed disabled:border-line disabled:text-ink-faint"
              >
                {isSubmitting ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                    Deleting...
                  </>
                ) : (
                  confirmLabel
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
