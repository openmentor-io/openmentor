import classNames from 'classnames'
import { Menu } from '@headlessui/react'
import type { MentorListItem } from '@/types'

export type MentorsSortOption = 'relevance' | 'sessions' | 'newest'

const SORT_LABELS: Record<MentorsSortOption, string> = {
  relevance: 'Relevance',
  sessions: 'Most sessions',
  newest: 'Newest',
}

const SORT_OPTIONS = Object.keys(SORT_LABELS) as MentorsSortOption[]

/**
 * Client-side sort of the visible catalog list.
 * - relevance: default order (as delivered by the API / useMentors)
 * - sessions: completed sessions, descending
 * - newest: NEW mentors first (stable within groups)
 */
export function sortMentors(mentors: MentorListItem[], sort: MentorsSortOption): MentorListItem[] {
  if (sort === 'sessions') {
    return [...mentors].sort((a, b) => (b.sessionsCount ?? 0) - (a.sessionsCount ?? 0))
  }
  if (sort === 'newest') {
    return [...mentors].sort((a, b) => Number(b.isNew) - Number(a.isNew))
  }
  return mentors
}

interface MentorsSortProps {
  value: MentorsSortOption
  onChange: (value: MentorsSortOption) => void
}

/**
 * "SORT: RELEVANCE ▾" mono control (design 01 §results meta) with a small
 * dropdown panel in the shared panel style.
 */
export default function MentorsSort({ value, onChange }: MentorsSortProps): JSX.Element {
  return (
    <Menu as="div" className="relative shrink-0">
      <Menu.Button className="meta-mono whitespace-nowrap text-ink-mute transition-colors duration-120 hover:text-ink">
        SORT: {SORT_LABELS[value].toUpperCase()} ▾
      </Menu.Button>

      <Menu.Items className="absolute right-0 z-20 mt-2 flex w-44 animate-dropdown-in flex-col gap-0.5 rounded-card border border-line bg-white p-2 shadow-dropdown focus:outline-none">
        {SORT_OPTIONS.map((option) => (
          <Menu.Item key={option}>
            {({ active }) => (
              <button
                type="button"
                className={classNames(
                  'rounded-[9px] px-3 py-[9px] text-left text-[13px]',
                  value === option
                    ? 'bg-brand-cobalt font-semibold text-white'
                    : classNames('font-medium text-ink', active && 'bg-surface')
                )}
                onClick={() => onChange(option)}
              >
                {SORT_LABELS[option]}
              </button>
            )}
          </Menu.Item>
        ))}
      </Menu.Items>
    </Menu>
  )
}
