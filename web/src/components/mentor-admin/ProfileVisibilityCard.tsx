/**
 * Profile visibility card for the mentor profile edit page.
 *
 * Lets an approved mentor toggle their profile between 'active' (shown in the
 * public catalog) and 'inactive' (hidden). Saves immediately on toggle with
 * optimistic UI and rollback on failure.
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
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
      <h2 className="text-lg font-semibold text-ink">Profile visibility</h2>

      <Switch.Group as="div" className="mt-4 flex items-center justify-between gap-4">
        <Switch.Label as="span" className="text-sm text-ink" passive>
          Show my profile in the mentor catalog
        </Switch.Label>
        <Switch
          checked={isActive}
          onChange={handleToggle}
          disabled={isSaving}
          className={classNames(
            isActive ? 'bg-brand-navy' : 'bg-gray-200',
            isSaving ? 'opacity-60 cursor-wait' : 'cursor-pointer',
            'relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-brand-cobalt focus:ring-offset-2'
          )}
        >
          <span
            aria-hidden="true"
            className={classNames(
              isActive ? 'translate-x-5' : 'translate-x-0',
              'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out'
            )}
          />
        </Switch>
      </Switch.Group>

      {!isActive && (
        <p className="mt-3 rounded-lg bg-surface p-3 text-sm text-gray-600">
          Your profile is hidden from the catalog. Mentees can&apos;t send you new requests, but you
          can still manage existing ones.
        </p>
      )}

      {error && (
        <p className="mt-3 text-sm text-red-600" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
