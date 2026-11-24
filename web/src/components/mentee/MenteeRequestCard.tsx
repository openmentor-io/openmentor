/**
 * One request in the mentee dashboard (D95): mentor, status, the mentee's own
 * message, and — depending on the group the API put it in — the mentor's
 * calendar link, the decline reason, or the review.
 *
 * The API decides what may be shown (no calendar link outside pending/active,
 * no mentor contact at all); this component only renders what it is given.
 */

import { useState } from 'react'
import Link from 'next/link'
import classNames from 'classnames'
import type { MenteeRequest, MenteeReview } from '@/types'
import { DECLINE_REASONS } from '@/types'
import { safeHttpUrl } from '@/lib/safe-url'
import StatusBadge from '@/components/mentor-admin/StatusBadge'
import { formatDate } from '@/components/mentor-admin/utils'
import MenteeReviewForm from './MenteeReviewForm'

/**
 * posthog-js's session-replay text mask class. The request text, decline
 * comment and review are free text written by people; `redact.ts` strips
 * capability shapes only, so it cannot protect them.
 */
export const REPLAY_MASK_CLASS = 'ph-mask'

interface MenteeRequestCardProps {
  request: MenteeRequest
  onReviewed: (requestId: string, review: MenteeReview) => void
}

function declineReasonLabel(reason?: string): string | null {
  if (!reason) return null
  return DECLINE_REASONS.find((r) => r.value === reason)?.label ?? null
}

function ReviewBlock({ review }: { review: MenteeReview }): JSX.Element {
  return (
    <div className={classNames('mt-4 rounded-field bg-surface p-4', REPLAY_MASK_CLASS)}>
      <p className="meta-mono my-0 text-ink-mute">Your review · {formatDate(review.createdAt)}</p>
      <p className="my-0 mt-2 whitespace-pre-line text-sm leading-relaxed text-ink">
        {review.mentorReview}
      </p>
      {review.platformReview && (
        <p className="my-0 mt-3 whitespace-pre-line text-sm text-ink-soft">
          <span className="font-semibold text-ink">About the platform: </span>
          {review.platformReview}
        </p>
      )}
      {review.improvements && (
        <p className="my-0 mt-2 whitespace-pre-line text-sm text-ink-soft">
          <span className="font-semibold text-ink">Suggestions: </span>
          {review.improvements}
        </p>
      )}
    </div>
  )
}

export default function MenteeRequestCard({
  request,
  onReviewed,
}: MenteeRequestCardProps): JSX.Element {
  const [reviewOpen, setReviewOpen] = useState(false)
  const [reviewVisible, setReviewVisible] = useState(false)
  const { mentor } = request
  const calendarUrl = safeHttpUrl(mentor.calendarUrl)
  const mentorName = mentor.available ? mentor.name : 'Mentor no longer on OpenMentor'
  const declineLabel = declineReasonLabel(request.declineReason)

  return (
    <article
      className={classNames(
        'rounded-card border border-line bg-white px-5 py-4 sm:px-[22px] sm:py-[18px]',
        request.group === 'pending' && 'border-l-4 border-l-brand-cobalt',
        request.group === 'closed' && 'opacity-75',
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          {mentor.slug ? (
            <Link
              href={`/mentor/${mentor.slug}`}
              className="font-name text-base font-bold tracking-[-0.015em] text-ink hover:text-brand-cobalt"
            >
              {mentorName}
            </Link>
          ) : (
            <span className="font-name text-base font-bold tracking-[-0.015em] text-ink">
              {mentorName}
            </span>
          )}
          <p className="meta-mono my-0 mt-1 text-ink-mute">
            Sent <time dateTime={request.createdAt}>{formatDate(request.createdAt)}</time>
            {request.statusChangedAt && request.status !== 'pending' && (
              <>
                {' · Updated '}
                <time dateTime={request.statusChangedAt}>
                  {formatDate(request.statusChangedAt)}
                </time>
              </>
            )}
          </p>
        </div>
        <StatusBadge status={request.status} className="flex-none" />
      </div>

      {request.details && (
        <p
          className={classNames(
            'my-0 mt-3 line-clamp-4 whitespace-pre-line text-[13.5px] leading-normal text-ink-soft',
            REPLAY_MASK_CLASS,
          )}
        >
          {request.details}
        </p>
      )}

      {calendarUrl && (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <a
            href={calendarUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="button-secondary"
          >
            Book a time in the mentor&apos;s calendar
          </a>
        </div>
      )}

      {request.status === 'declined' && (declineLabel || request.declineComment) && (
        <div
          className={classNames(
            'mt-4 rounded-field bg-surface p-4 text-sm text-ink-soft',
            REPLAY_MASK_CLASS,
          )}
        >
          {declineLabel && <p className="my-0 font-semibold text-ink">{declineLabel}</p>}
          {request.declineComment && (
            <p className="my-0 mt-1 whitespace-pre-line">{request.declineComment}</p>
          )}
        </div>
      )}

      {request.review && (
        <>
          <button
            type="button"
            onClick={() => setReviewVisible(!reviewVisible)}
            aria-expanded={reviewVisible}
            className="mt-4 text-[13px] font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
          >
            {reviewVisible ? 'Hide your review' : 'View your review'}
          </button>
          {reviewVisible && <ReviewBlock review={request.review} />}
        </>
      )}

      {request.canReview && !reviewOpen && (
        <div className="mt-4">
          <button type="button" onClick={() => setReviewOpen(true)} className="button">
            Leave a review
          </button>
        </div>
      )}

      {request.canReview && reviewOpen && (
        <MenteeReviewForm
          requestId={request.id}
          mentorName={mentor.name}
          onCancel={() => setReviewOpen(false)}
          onSubmitted={(review) => {
            setReviewOpen(false)
            setReviewVisible(true)
            onReviewed(request.id, review)
          }}
        />
      )}
    </article>
  )
}
