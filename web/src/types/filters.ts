/**
 * Filter configuration types
 */

import type { MentorListItem } from './mentor'

/**
 * Topic tab category: a label shown as a tab and the tags it aggregates
 */
export interface FilterCategory {
  label: string
  tags: string[]
}

/**
 * Filter configuration object
 */
export interface FiltersConfig {
  tags: string[]
  byTags: {
    development: string[]
    management: string[]
    ops: string[]
    hr: string[]
    marketing: string[]
    rest: string[]
  }
  /** Ordered topic tabs for the catalog tab bar (redesign Phase A) */
  categories: FilterCategory[]
  price: string[]
  experience: Record<string, string>
  /** Price filter buckets: label -> predicate over the free-text price (DECISIONS D3) */
  byPrice: Record<string, (price: string) => boolean>
}

/**
 * Generic filter state for array values
 */
export interface FilterState<T> {
  values: T
  set: (values: T) => void
  reset: () => void
}

/**
 * Boolean filter state
 */
export interface BooleanFilterState {
  value: boolean
  set: (value: boolean) => void
  reset: () => void
}

/**
 * Applied filters state (from useMentors hook)
 */
export interface AppliedFilters {
  tags: FilterState<string[]>
  /** Single-select topic tab (label from FiltersConfig.categories) */
  category: FilterState<string | undefined>
  experience: FilterState<string[]>
  price: FilterState<string | undefined>
  noSessions: BooleanFilterState
  newMentor: BooleanFilterState
  count: () => number
}

/**
 * useMentors hook return type
 */
export type UseMentorsReturn = [
  MentorListItem[], // mentors
  string, // searchInput
  boolean, // hasMoreMentors
  (value: string) => void, // setSearchInput
  () => void, // showMoreMentors
  AppliedFilters // appliedFilters
]
