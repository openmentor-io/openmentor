/**
 * Request Details Page (design 08)
 *
 * Single-column detail view: mentee header card with initials avatar and
 * status pill, "their message" card with meta row, review card, and the
 * status actions (advance / decline with note). All existing status
 * behaviors are kept.
 */

import { useState, useEffect } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faStar } from '@fortawesome/free-solid-svg-icons'
import classNames from 'classnames'
import type { MentorClientRequest, RequestStatus, DeclineReasonValue } from '@/types'
import { STATUS_TRANSITIONS, STATUS_LABELS, ACTIVE_STATUSES } from '@/types'
import {
  MentorAuthProvider,
  useMentorAuth,
  MentorAdminLayout,
  StatusBadge,
  DeclineModal,
  formatDateTime,
  formatRelativeTime,
  nameInitials,
} from '@/components/mentor-admin'
import { getRequestById, updateRequestStatus, declineRequest } from '@/lib/mentor-admin-api'

/**
 * Get the next status in the workflow
 */
function getNextStatus(currentStatus: RequestStatus): RequestStatus | null {
  const transitions = STATUS_TRANSITIONS[currentStatus]
  // Return the first non-declined transition (the main workflow transition)
  return transitions.find((s) => s !== 'declined') || null
}

/**
 * Check if status can be declined
 */
function canDecline(status: RequestStatus): boolean {
  return STATUS_TRANSITIONS[status].includes('declined')
}

interface MetaItemProps {
  label: string
  value: string
  href?: string
}

function MetaItem({ label, value, href }: MetaItemProps): JSX.Element {
  return (
    <div className="min-w-0">
      <div className="text-xs text-ink-soft">{label}</div>
      {href ? (
        <a
          href={href}
          className="block truncate text-[13px] font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
        >
          {value}
        </a>
      ) : (
        <div className="truncate text-[13px] font-semibold text-ink">{value}</div>
      )}
    </div>
  )
}

function RequestDetailsContent(): JSX.Element {
  const router = useRouter()
  const { id } = router.query
  const { isAuthenticated, isLoading: authLoading } = useMentorAuth()
  const [request, setRequest] = useState<MentorClientRequest | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isUpdatingStatus, setIsUpdatingStatus] = useState(false)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [showDeclineModal, setShowDeclineModal] = useState(false)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/mentor/login')
    }
  }, [authLoading, isAuthenticated, router])

  // Load request
  useEffect(() => {
    if (!isAuthenticated || !id) return

    const loadRequest = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const data = await getRequestById(id as string)
        if (!data) {
          setError('Request not found')
        } else {
          setRequest(data)
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load the request')
      } finally {
        setIsLoading(false)
      }
    }

    loadRequest()
  }, [isAuthenticated, id])

  const handleStatusChange = async (newStatus: RequestStatus): Promise<void> => {
    if (!request) return

    setIsUpdatingStatus(true)
    setStatusError(null)

    try {
      const updated = await updateRequestStatus(request.id, newStatus)
      setRequest(updated)
    } catch (err) {
      setStatusError(err instanceof Error ? err.message : 'Failed to update the status')
    } finally {
      setIsUpdatingStatus(false)
    }
  }

  const handleDecline = async (reason: DeclineReasonValue, comment?: string): Promise<void> => {
    if (!request) return

    const updated = await declineRequest(request.id, { reason, comment })
    setRequest(updated)
  }

  // Determine which list to go back to
  const backLink = request && ACTIVE_STATUSES.includes(request.status) ? '/mentor' : '/mentor/past'
  const nextStatus = request ? getNextStatus(request.status) : null

  // Show loading while checking auth
  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <>
      <Head>
        <title>{request ? `${request.name} — Request` : 'Request'} — openmentor.io</title>
      </Head>

      <MentorAdminLayout>
        <div className="max-w-[820px]">
          {/* Back link */}
          <div className="mb-4">
            <Link
              href={backLink}
              className="text-[13px] font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              ← All requests
            </Link>
          </div>

          {/* Loading state */}
          {isLoading && (
            <div className="flex flex-col items-center justify-center py-12">
              <FontAwesomeIcon
                icon={faCircleNotch}
                className="mb-3 animate-spin text-2xl text-brand-cobalt"
              />
              <p className="my-0 text-sm text-ink-soft">Loading request...</p>
            </div>
          )}

          {/* Error state */}
          {error && !isLoading && (
            <div className="rounded-panel border border-danger/40 bg-white p-6 text-center">
              <p className="my-0 mb-4 text-sm font-medium text-danger">{error}</p>
              <Link href="/mentor" className="button">
                Back to requests
              </Link>
            </div>
          )}

          {/* Request details */}
          {!isLoading && !error && request && (
            <div className="flex animate-rise-in flex-col gap-3.5">
              {/* Header card */}
              <div className="flex items-center gap-5 rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
                <div
                  aria-hidden="true"
                  className="flex h-14 w-14 flex-none items-center justify-center rounded-full bg-brand-navy font-name text-xl font-bold text-white"
                >
                  {nameInitials(request.name)}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-baseline gap-3">
                    <h1 className="font-name text-xl font-bold normal-case tracking-[-0.015em] text-ink sm:text-2xl">
                      {request.name}
                    </h1>
                    <StatusBadge status={request.status} />
                  </div>
                  <div className="mt-1 truncate text-sm text-ink-soft">
                    {request.email} · requested {formatRelativeTime(request.createdAt)}
                  </div>
                </div>
              </div>

              {/* Message card */}
              <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
                <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                  Their message
                </div>
                <p className="my-0 max-w-[640px] whitespace-pre-wrap text-[14.5px] leading-relaxed text-ink">
                  {request.details}
                </p>

                <div className="mt-4 grid grid-cols-2 gap-4 border-t border-line pt-4 sm:grid-cols-4 sm:gap-6">
                  <MetaItem label="Level" value={request.level} />
                  <MetaItem label="Email" value={request.email} href={`mailto:${request.email}`} />
                  {request.contact && <MetaItem label="Contact" value={request.contact} />}
                  <MetaItem label="Received" value={formatDateTime(request.createdAt)} />
                </div>
              </div>

              {/* Review card (if exists) */}
              {request.review && (
                <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
                  <div className="mb-2.5 flex items-center gap-2 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                    <FontAwesomeIcon icon={faStar} className="text-brand-mint" />
                    Review
                  </div>
                  <p className="my-0 whitespace-pre-wrap text-[14.5px] leading-relaxed text-ink">
                    {request.review}
                  </p>
                </div>
              )}

              {/* Status timeline card */}
              <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
                <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                  Status
                </div>
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 sm:gap-6">
                  <MetaItem label="Current status" value={STATUS_LABELS[request.status]} />
                  <MetaItem label="Status changed" value={formatDateTime(request.statusChangedAt)} />
                  {request.scheduledAt && (
                    <MetaItem label="Scheduled" value={formatDateTime(request.scheduledAt)} />
                  )}
                </div>
              </div>

              {/* Actions card */}
              <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
                <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                  Next steps
                </div>

                {(nextStatus || canDecline(request.status)) && (
                  <p className="my-0 mb-3.5 text-[13px] leading-normal text-ink-soft">
                    Reply to {request.name.split(' ')[0]} by email from your own inbox — status
                    updates here keep your dashboard in sync and notify the mentee.
                  </p>
                )}

                {/* Status error */}
                {statusError && (
                  <p className="my-0 mb-3 animate-shake text-sm font-medium text-danger" role="alert">
                    {statusError}
                  </p>
                )}

                <div className="flex flex-wrap items-center gap-2.5">
                  {/* Next status button */}
                  {nextStatus && (
                    <button
                      onClick={() => handleStatusChange(nextStatus)}
                      disabled={isUpdatingStatus}
                      className={classNames('button', isUpdatingStatus && 'opacity-60')}
                    >
                      {isUpdatingStatus ? (
                        <>
                          <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                          Updating...
                        </>
                      ) : (
                        `Mark as "${STATUS_LABELS[nextStatus]}"`
                      )}
                    </button>
                  )}

                  {/* Decline button */}
                  {canDecline(request.status) && (
                    <button
                      onClick={() => setShowDeclineModal(true)}
                      disabled={isUpdatingStatus}
                      className="button-destructive disabled:opacity-50"
                    >
                      Decline…
                    </button>
                  )}

                  {/* No actions available */}
                  {!nextStatus && !canDecline(request.status) && (
                    <p className="my-0 py-1 text-sm text-ink-soft">
                      The request is closed — no actions available.
                    </p>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Decline Modal */}
        {request && (
          <DeclineModal
            isOpen={showDeclineModal}
            onClose={() => setShowDeclineModal(false)}
            onConfirm={handleDecline}
            menteName={request.name}
          />
        )}
      </MentorAdminLayout>
    </>
  )
}

export default function RequestDetailsPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <RequestDetailsContent />
    </MentorAuthProvider>
  )
}
