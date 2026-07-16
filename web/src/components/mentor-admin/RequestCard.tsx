/**
 * Request Card component
 *
 * A single request row in the inbox list (design 07): initials circle,
 * mentee name in Schibsted, mono timestamp, one-line message preview and
 * a mono status pill. Pending rows carry the 4px cobalt "unread" bar;
 * closed rows are dimmed until hover.
 */

import Link from 'next/link'
import classNames from 'classnames'
import type { MentorClientRequest, RequestStatus } from '@/types'
import StatusBadge from './StatusBadge'
import { formatCompactTime, nameInitials } from './utils'

interface RequestCardProps {
  request: MentorClientRequest
}

/** Initials-circle fill per request status (design 07 row set). */
const AVATAR_CLASSES: Record<RequestStatus, string> = {
  pending: 'bg-brand-navy text-white',
  contacted: 'bg-brand-cobalt text-white',
  working: 'bg-brand-mint text-white',
  done: 'bg-ink-soft text-white',
  declined: 'bg-line text-ink-mute',
  unavailable: 'bg-line text-ink-mute',
}

export default function RequestCard({ request }: RequestCardProps): JSX.Element {
  const isPending = request.status === 'pending'
  const isClosed =
    request.status === 'done' || request.status === 'declined' || request.status === 'unavailable'

  return (
    <Link
      href={`/mentor/requests/${request.id}`}
      className={classNames(
        'flex items-center gap-4 rounded-card border border-line bg-white px-5 py-4 transition-all duration-180 hover:shadow-card-hover sm:gap-[18px] sm:px-[22px] sm:py-[18px]',
        isPending && 'border-l-4 border-l-brand-cobalt',
        request.status === 'done' && 'opacity-75 hover:opacity-100',
        (request.status === 'declined' || request.status === 'unavailable') &&
          'opacity-60 hover:opacity-100'
      )}
    >
      {/* Initials circle */}
      <div
        aria-hidden="true"
        className={classNames(
          'flex h-11 w-11 flex-none items-center justify-center rounded-full font-name text-base font-bold',
          AVATAR_CLASSES[request.status]
        )}
      >
        {nameInitials(request.name)}
      </div>

      {/* Name, timestamp, preview */}
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2.5">
          <span className="truncate font-name text-base font-bold tracking-[-0.015em] text-ink">
            {request.name}
          </span>
          <time dateTime={request.createdAt} className="meta-mono flex-none text-ink-mute">
            {formatCompactTime(request.createdAt)}
          </time>
        </div>
        <p className="my-0 mt-0.5 truncate text-[13.5px] leading-normal text-ink-soft">
          {request.details}
        </p>
      </div>

      {/* Status pill (hidden on the smallest screens for closed rows) */}
      <StatusBadge
        status={request.status}
        className={classNames('flex-none', isClosed && 'hidden sm:inline-flex')}
      />
    </Link>
  )
}
