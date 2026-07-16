/**
 * Sort toggle component for request lists (secondary-pill styling).
 */

import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faArrowUp, faArrowDown } from '@fortawesome/free-solid-svg-icons'
import type { SortOrder } from '@/types'

interface SortToggleProps {
  value: SortOrder
  onChange: (order: SortOrder) => void
}

export default function SortToggle({ value, onChange }: SortToggleProps): JSX.Element {
  const toggle = (): void => {
    onChange(value === 'newest' ? 'oldest' : 'newest')
  }

  return (
    <button
      onClick={toggle}
      className="inline-flex items-center rounded-full border-[1.5px] border-line bg-white px-4 py-2 text-xs font-semibold text-brand-navy transition-colors duration-120 hover:border-brand-cobalt/45"
      title={value === 'newest' ? 'Newest first' : 'Oldest first'}
    >
      <FontAwesomeIcon
        icon={value === 'newest' ? faArrowDown : faArrowUp}
        className="mr-2 text-ink-soft"
      />
      {value === 'newest' ? 'Newest first' : 'Oldest first'}
    </button>
  )
}
