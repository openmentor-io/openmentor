/**
 * Admin — single mentee request.
 *
 * Read-only view of what the mentee sent plus the admin status override. The
 * override is deliberately unrestricted: unlike the mentor's own inbox it can
 * move a request BACK out of a terminal status ("done" / "declined" /
 * "unavailable"), which is the reason this page exists. It never emails the
 * mentee — the backend keeps admin edits silent.
 */

import { useEffect, useState } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faStar } from '@fortawesome/free-solid-svg-icons'
import type { AdminMentorDetails, MentorClientRequest, RequestStatus } from '@/types'
import { ALL_REQUEST_STATUSES, STATUS_LABELS } from '@/types'
import { pageTitle } from '@/config/seo'
import { AdminAuthProvider, AdminLayout, useAdminAuth } from '@/components/admin-moderation'
import {
  MetaItem,
  StatusBadge,
  formatDateTime,
  formatRelativeTime,
} from '@/components/mentor-admin'
import {
  getModerationMentorById,
  getModerationMentorRequest,
  updateModerationRequestStatus,
} from '@/lib/admin-moderation-api'

const labelClass = 'mb-1 block text-[13px] font-semibold text-ink'

/** statusChangedAt is NULL on rows that predate status tracking. */
function formatOptionalDateTime(value: string | null): string {
  return value ? formatDateTime(value) : '—'
}

function AdminRequestDetailsContent(): JSX.Element {
  const router = useRouter()
  const { id, requestId } = router.query
  const mentorId = Array.isArray(id) ? id[0] : id
  const clientRequestId = Array.isArray(requestId) ? requestId[0] : requestId
  const { isAuthenticated, isLoading: authLoading, session } = useAdminAuth()

  const [mentor, setMentor] = useState<AdminMentorDetails | null>(null)
  const [request, setRequest] = useState<MentorClientRequest | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [selectedStatus, setSelectedStatus] = useState<RequestStatus | ''>('')
  const [isSaving, setIsSaving] = useState(false)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [savedStatus, setSavedStatus] = useState<RequestStatus | null>(null)

  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/admin/login')
      return
    }
    if (!authLoading && session && session.role !== 'admin') {
      router.replace('/admin/mentors/pending')
    }
  }, [authLoading, isAuthenticated, router, session])

  useEffect(() => {
    if (!isAuthenticated || session?.role !== 'admin' || !mentorId || !clientRequestId) return

    let mounted = true
    const load = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const [mentorData, requestData] = await Promise.all([
          getModerationMentorById(mentorId),
          getModerationMentorRequest(mentorId, clientRequestId),
        ])
        if (!mounted) return
        if (!requestData) {
          setError('Request not found')
          return
        }
        setMentor(mentorData)
        setRequest(requestData)
        setSelectedStatus(requestData.status)
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : 'Failed to load the request')
        }
      } finally {
        if (mounted) setIsLoading(false)
      }
    }

    load()
    return () => {
      mounted = false
    }
  }, [isAuthenticated, session, mentorId, clientRequestId])

  const onSaveStatus = async (): Promise<void> => {
    if (!request || !mentorId || !selectedStatus || selectedStatus === request.status) return

    setIsSaving(true)
    setStatusError(null)
    setSavedStatus(null)
    try {
      const updated = await updateModerationRequestStatus(mentorId, request.id, selectedStatus)
      setRequest(updated)
      setSelectedStatus(updated.status)
      setSavedStatus(updated.status)
    } catch (err) {
      setStatusError(err instanceof Error ? err.message : 'Failed to update the status')
    } finally {
      setIsSaving(false)
    }
  }

  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  const isDirty = Boolean(request && selectedStatus && selectedStatus !== request.status)

  return (
    <AdminLayout title="Request">
      <Head>
        <title>{pageTitle(request ? `${request.name} — Request` : 'Request')}</title>
        <meta name="robots" content="noindex,follow" />
      </Head>

      {mentorId && (
        <div className="mb-4 flex flex-wrap items-center gap-4">
          <Link
            href={`/admin/mentors/${encodeURIComponent(mentorId)}/requests`}
            className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
          >
            ← All requests
          </Link>
          <Link
            href={`/admin/mentors/${encodeURIComponent(mentorId)}`}
            className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
          >
            Mentor profile
          </Link>
        </div>
      )}

      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <FontAwesomeIcon
            icon={faCircleNotch}
            className="animate-spin text-2xl text-brand-cobalt"
          />
        </div>
      )}

      {error && !isLoading && (
        <div className="rounded-card border border-danger/40 bg-white p-4 text-sm font-medium text-danger">
          {error}
        </div>
      )}

      {!isLoading && !error && request && (
        <div className="max-w-[820px] space-y-3.5">
          {/* Header */}
          <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
            <div className="flex flex-wrap items-baseline gap-3">
              <h2 className="my-0 font-name text-xl font-bold normal-case tracking-[-0.015em] text-ink">
                {request.name}
              </h2>
              <StatusBadge status={request.status} />
            </div>
            <div className="mt-1 text-sm text-ink-soft">
              <a
                href={`mailto:${request.email}`}
                className="break-all text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
              >
                {request.email}
              </a>{' '}
              · requested {formatRelativeTime(request.createdAt)}
              {mentor ? ` · for ${mentor.name}` : ''}
            </div>
          </div>

          {/* Message */}
          <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
            <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
              Their message
            </div>
            <p className="my-0 whitespace-pre-wrap text-[14.5px] leading-relaxed text-ink">
              {request.details}
            </p>
            <div className="mt-4 grid grid-cols-2 gap-4 border-t border-line pt-4 sm:grid-cols-4 sm:gap-6">
              <MetaItem label="Level" value={request.level || '—'} />
              <MetaItem
                label="Email"
                value={request.email}
                href={`mailto:${request.email}`}
                copyable
              />
              {request.contact ? (
                <MetaItem label="Contact" value={request.contact} copyable />
              ) : (
                <MetaItem label="Contact" value="—" />
              )}
              <MetaItem label="Received" value={formatDateTime(request.createdAt)} />
            </div>
          </div>

          {/* Review, when the mentee left one */}
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

          {/* Decline details, when the mentor declined */}
          {(request.declineReason || request.declineComment) && (
            <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
              <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                Decline details
              </div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <MetaItem label="Reason" value={request.declineReason || '—'} />
              </div>
              {request.declineComment && (
                <p className="my-0 mt-3 whitespace-pre-wrap text-[14.5px] leading-relaxed text-ink">
                  {request.declineComment}
                </p>
              )}
            </div>
          )}

          {/* Status + admin override */}
          <div className="rounded-panel border border-line bg-white p-5 sm:px-7 sm:py-6">
            <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
              Status
            </div>
            <div className="mb-4 grid grid-cols-2 gap-4 sm:grid-cols-4 sm:gap-6">
              <MetaItem label="Current status" value={STATUS_LABELS[request.status]} />
              <MetaItem
                label="Status changed"
                value={formatOptionalDateTime(request.statusChangedAt)}
              />
              {request.scheduledAt && (
                <MetaItem label="Scheduled" value={formatDateTime(request.scheduledAt)} />
              )}
              <MetaItem label="Last modified" value={formatDateTime(request.modifiedAt)} />
            </div>

            <div className="border-t border-line pt-4">
              <label htmlFor="request-status" className={labelClass}>
                Change status
              </label>
              <p className="my-0 mb-2 text-[13px] text-ink-soft">
                Admins can set any status, including moving a closed request back into the
                mentor&apos;s active list. No email is sent to the mentee.
              </p>
              <select
                id="request-status"
                value={selectedStatus}
                onChange={(e) => {
                  setSelectedStatus(e.target.value as RequestStatus)
                  setStatusError(null)
                  setSavedStatus(null)
                }}
                disabled={isSaving}
                className="field"
              >
                {ALL_REQUEST_STATUSES.map((status) => (
                  <option key={status} value={status}>
                    {STATUS_LABELS[status]}
                  </option>
                ))}
              </select>

              {statusError && (
                <p className="my-0 mt-2 text-sm font-medium text-danger" role="alert">
                  {statusError}
                </p>
              )}

              <div className="mt-3 flex flex-wrap items-center gap-3">
                <button
                  type="button"
                  onClick={onSaveStatus}
                  disabled={!isDirty || isSaving}
                  className="button disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {isSaving ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                      Updating...
                    </>
                  ) : (
                    'Update status'
                  )}
                </button>
                {savedStatus && !isDirty && (
                  <span className="text-sm font-medium text-mint-ink">
                    Status set to &quot;{STATUS_LABELS[savedStatus]}&quot;.
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className="meta-mono text-ink-mute">Request ID: {request.id}</div>
        </div>
      )}
    </AdminLayout>
  )
}

export default function AdminRequestDetailsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <AdminRequestDetailsContent />
    </AdminAuthProvider>
  )
}
