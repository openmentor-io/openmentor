/**
 * Danger-zone card on the mentor profile edit page (D70).
 *
 * Sits LAST on the page, below the profile form, because it is the one control
 * there whose result cannot be undone by the mentor — nothing above it should
 * be reachable only by scrolling past this.
 *
 * Deleting revokes the mentor's session server-side, so after a successful
 * delete every subsequent request 401s. The card therefore hands control back
 * to the page via onDeleted rather than trying to refresh anything itself.
 */

import { useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTriangleExclamation } from '@fortawesome/free-solid-svg-icons'
import { DeleteProfileDialog } from '@/components'
import { captureException } from '@/lib/posthog'

interface DeleteProfileCardProps {
  /** The mentor's username (slug) — what they must retype to confirm. */
  username: string
  /**
   * Whether the visibility toggle is on the page. Only approved profiles get
   * one, and pointing a draft mentor at a control they cannot see is worse
   * than not offering the alternative at all.
   */
  canHideInstead?: boolean
  /** Called after the profile has been deleted server-side. */
  onDeleted: () => void
}

export default function DeleteProfileCard({
  username,
  canHideInstead = false,
  onDeleted,
}: DeleteProfileCardProps): JSX.Element {
  const [isDialogOpen, setIsDialogOpen] = useState(false)

  const onConfirm = async (): Promise<void> => {
    try {
      const response = await fetch('/api/mentor/profile/delete', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username }),
      })

      const data = await response.json().catch(() => null)

      if (!response.ok || !data?.success) {
        // Surface the server's message: the two it can send here — a username
        // mismatch and an already-deleted profile — are both things the mentor
        // can act on, and a generic failure would hide that.
        throw new Error(data?.error || 'Failed to delete your profile. Please try again.')
      }

      setIsDialogOpen(false)
      onDeleted()
    } catch (e) {
      if (e instanceof Error) {
        captureException(e, { page: 'edit-profile', action: 'delete-profile' })
      }
      throw e
    }
  }

  return (
    <div className="rounded-panel border-[1.5px] border-danger/30 bg-white p-5 sm:px-[26px] sm:py-[22px]">
      <div className="flex items-start gap-4">
        <FontAwesomeIcon
          icon={faTriangleExclamation}
          className="mt-0.5 text-xl text-danger"
          aria-hidden="true"
        />
        <div className="min-w-0 flex-1">
          <p className="my-0 font-name text-base font-bold tracking-[-0.015em] text-ink">
            Delete your profile
          </p>
          <p className="my-0 mt-1 text-[13px] leading-normal text-ink-soft">
            Your profile disappears from the site, your login link stops working, and any
            outstanding review invitations are cancelled. This cannot be undone — only an admin can
            bring a deleted profile back, and it is eventually erased for good along with your
            requests and reviews.
          </p>
          {canHideInstead && (
            <p className="my-0 mt-3 text-[13px] leading-normal text-ink-mute">
              Only want to step away for a while? Switch your profile to hidden with the visibility
              toggle above instead — that takes you out of search and keeps everything else.
            </p>
          )}
          <button
            type="button"
            onClick={() => setIsDialogOpen(true)}
            className="button-destructive mt-4"
          >
            Delete profile
          </button>
        </div>
      </div>

      <DeleteProfileDialog
        isOpen={isDialogOpen}
        onClose={() => setIsDialogOpen(false)}
        onConfirm={onConfirm}
        username={username}
        title="Delete your profile"
      >
        <p className="my-0">
          Your public page will return a 404, your login link will stop working, and any review
          invitations still with your mentees will be cancelled.
        </p>
        <p className="my-0 mt-2">
          Your profile and everything attached to it — requests and reviews — are then permanently
          erased. If you change your mind, contact us as soon as possible: once it is erased it
          cannot be undone.
        </p>
      </DeleteProfileDialog>
    </div>
  )
}
