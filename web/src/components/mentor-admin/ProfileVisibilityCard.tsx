/**
 * Profile visibility card for the mentor profile edit page (design 09).
 *
 * Lets an approved mentor toggle their profile between 'active' (shown in the
 * public catalog) and 'inactive' (hidden). Saves immediately on toggle with
 * optimistic UI and rollback on failure. Visible state gets the mint border +
 * soft ring; hidden state falls back to the quiet line border.
 */

import { useState } from 'react'
import { Switch } from '@headlessui/react'
import classNames from 'classnames'
import { captureException } from '@/lib/posthog'

type VisibilityStatus = 'active' | 'inactive'

interface ProfileVisibilityCardProps {
  initialStatus: VisibilityStatus
  onSuccess?: (status: VisibilityStatus) => void
}

export default function ProfileVisibilityCard({
  initialStatus,
  onSuccess,
}: ProfileVisibilityCardProps): JSX.Element {
  const [status, setStatus] = useState<VisibilityStatus>(initialStatus)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isActive = status === 'active'

  const handleToggle = async (checked: boolean): Promise<void> => {
    if (isSaving) return

    const previousStatus = status
    const nextStatus: VisibilityStatus = checked ? 'active' : 'inactive'

    // Optimistic update
    setStatus(nextStatus)
    setError(null)
    setIsSaving(true)

    try {
      const response = await fetch('/api/mentor/profile/status', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ status: nextStatus }),
      })

      const data = await response.json().catch(() => null)

      if (!response.ok || !data?.success) {
        throw new Error(data?.error || 'Failed to update profile visibility')
      }

      if (onSuccess) {
        onSuccess(nextStatus)
      }
    } catch (e) {
      // Roll back the optimistic update
      setStatus(previousStatus)
      setError('Failed to update profile visibility. Please try again.')
      if (e instanceof Error) {
        captureException(e, { page: 'edit-profile', action: 'visibility-toggle' })
      }
      console.error('Profile visibility toggle error:', e)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <div
      className={classNames(
        'rounded-panel bg-white p-5 transition-all duration-180 sm:px-[26px] sm:py-[22px]',
        isActive
          ? 'border-[1.5px] border-brand-mint shadow-[0_0_0_4px_rgba(23,195,178,0.08)]'
          : 'border-[1.5px] border-line'
      )}
    >
      <Switch.Group as="div" className="flex items-center gap-4 sm:gap-5">
        <Switch
          checked={isActive}
          onChange={handleToggle}
          disabled={isSaving}
          className={classNames(
            isActive ? 'bg-brand-mint' : 'bg-line',
            isSaving ? 'cursor-wait opacity-60' : 'cursor-pointer',
            'relative inline-flex h-7 w-[52px] shrink-0 rounded-full transition-colors duration-180 ease-out'
          )}
        >
          <span
            aria-hidden="true"
            className={classNames(
              isActive ? 'translate-x-[27px]' : 'translate-x-[3px]',
              'pointer-events-none mt-[3px] inline-block h-[22px] w-[22px] transform rounded-full bg-white shadow transition duration-180 ease-out'
            )}
          />
        </Switch>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-2.5">
            <Switch.Label as="span" className="font-name text-base font-bold text-ink" passive>
              {isActive ? 'Your profile is visible' : 'Your profile is hidden'}
            </Switch.Label>
            {isActive ? (
              <span className="rounded-full bg-brand-mint/15 px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-[0.05em] text-mint-ink">
                Live in search
              </span>
            ) : (
              <span className="rounded-full bg-surface-deep px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-[0.05em] text-ink-mute">
                Not in search
              </span>
            )}
          </div>
          <p className="my-0 mt-1 text-[13px] leading-normal text-ink-soft">
            {isActive
              ? 'Mentees can find you in the catalog and send requests. Turn this off any time — your data stays, you just disappear from search.'
              : "Your profile is hidden from the catalog. Mentees can't send you new requests, but you can still manage existing ones."}
          </p>
        </div>
      </Switch.Group>

      {error && (
        <p className="my-0 mt-3 text-sm font-medium text-danger" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
