/**
 * Status Badge component
 *
 * Request status pill (design 07/08 + component sheet §pills): Plex Mono,
 * CAPS, 999 radius. Colors come from STATUS_COLORS.
 */

import classNames from 'classnames'
import type { RequestStatus } from '@/types'
import { STATUS_LABELS, STATUS_COLORS } from '@/types'

interface StatusBadgeProps {
  status: RequestStatus
  className?: string
}

/** Neutral pill for a status the UI does not know (see below). */
const UNKNOWN_STATUS_COLORS = { bg: 'bg-surface-deep', text: 'text-ink-mute' }

export default function StatusBadge({ status, className }: StatusBadgeProps): JSX.Element {
  // `status` is typed but arrives as JSON, so the type is no guarantee at
  // runtime: the database's status CHECK constraint is the real source of
  // truth and has held values the UI did not list (this is how 'reschedule'
  // crashed the admin request list). Render the raw value in a neutral pill
  // rather than dereferencing an undefined map entry and taking down the page.
  const colors = STATUS_COLORS[status] || UNKNOWN_STATUS_COLORS
  const label = STATUS_LABELS[status] || status

  return (
    <span
      className={classNames(
        'inline-flex items-center rounded-full px-3 py-1.5 font-mono text-[11px] font-bold uppercase tracking-[0.05em]',
        colors.bg,
        colors.text,
        className
      )}
    >
      {label}
    </span>
  )
}
