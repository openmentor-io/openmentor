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

export default function StatusBadge({ status, className }: StatusBadgeProps): JSX.Element {
  const colors = STATUS_COLORS[status]
  const label = STATUS_LABELS[status]

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
