import classNames from 'classnames'
import { useEffect } from 'react'
import allFilters from '@/config/filters'
import analytics from '@/lib/analytics'
import FilterGroupDropdown from './FilterGroupDropdown'
import type { AppliedFilters } from '@/types'

interface MentorsFiltersProps {
  appliedFilters: AppliedFilters
}

interface MultiValueFilter {
  values: string[]
  set: (values: string[]) => void
  reset: () => void
}

interface SingleValueFilter {
  values: string | undefined
  set: (value: string | undefined) => void
  reset: () => void
}

/**
 * Catalog filter tab bar (redesign Phase A): Experience + Price dropdown
 * pills, a divider, then single-select topic tabs mapped from the tag
 * taxonomy. Scrolls horizontally on small screens.
 */
export default function MentorsFilters(props: MentorsFiltersProps): JSX.Element {
  const { appliedFilters } = props

  useEffect(() => {
    if (window?.location?.hash?.startsWith('#tags:')) {
      const data = window?.location?.hash.split(':')
      let newTags = data[1] ? data[1].split('|').map((t) => decodeURI(t)) : []
      newTags = newTags.filter((item) => allFilters.tags.includes(item))

      appliedFilters.tags.set(newTags)

      if (newTags.length > 0) {
        analytics.event(analytics.events.MENTOR_FILTERS_INITIALIZED_FROM_URL, {
          tags_count: newTags.length,
        })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // Intentionally run once on mount - read URL hash on initial load

  const onClickExperience = (experience: string): void => {
    onClickFilterMultiple(experience, appliedFilters.experience, 'experience')
  }

  const onClickPrice = (price: string): void => {
    onClickFilterSingle(price, appliedFilters.price, 'price')
  }

  const onClickCategory = (category: string): void => {
    onClickFilterSingle(category, appliedFilters.category, 'category')
  }

  const onClickFilterMultiple = (
    newValue: string,
    filter: MultiValueFilter,
    filterType: string
  ): string[] => {
    let newValues: string[] = []

    if (filter.values.includes(newValue)) {
      newValues = filter.values.filter((item) => item !== newValue)

      analytics.event(analytics.events.MENTOR_FILTER_CHANGED, {
        filter_type: filterType,
        filter_value: newValue,
        action: 'removed',
      })
    } else {
      newValues = [...filter.values, newValue]

      analytics.event(analytics.events.MENTOR_FILTER_CHANGED, {
        filter_type: filterType,
        filter_value: newValue,
        action: 'added',
      })
    }

    filter.set(newValues)

    return newValues
  }

  const onClickFilterSingle = (
    newValue: string,
    filter: SingleValueFilter,
    filterType: string
  ): string => {
    if (filter.values === newValue) {
      filter.set(undefined)

      analytics.event(analytics.events.MENTOR_FILTER_CHANGED, {
        filter_type: filterType,
        filter_value: newValue,
        action: 'removed',
      })
    } else {
      filter.set(newValue)

      analytics.event(analytics.events.MENTOR_FILTER_CHANGED, {
        filter_type: filterType,
        filter_value: newValue,
        action: 'added',
      })
    }

    return newValue
  }

  return (
    <div className="flex flex-wrap items-center gap-y-2 sm:flex-nowrap">
      <div className="flex shrink-0 items-center">
        <FilterGroupDropdown
          title="Experience"
          values={Object.keys(allFilters.experience)}
          onFilterSelect={onClickExperience}
          allSelectedValues={appliedFilters.experience.values}
        />

        <FilterGroupDropdown
          title="Price"
          values={Object.keys(allFilters.byPrice)}
          onFilterSelect={onClickPrice}
          allSelectedValues={appliedFilters.price.values}
          multiSelect={false}
        />
      </div>

      <div className="mx-3 hidden h-6 w-px shrink-0 bg-line sm:block" />

      {/* Mobile: full-bleed horizontal scroll (container has 1rem padding),
          hidden scrollbar; the Experience/Price dropdowns stay outside this
          overflow container so their menus are never clipped. */}
      <div className="no-scrollbar -mx-4 flex w-full items-center overflow-x-auto px-4 py-1 sm:mx-0 sm:w-auto sm:px-0 sm:py-0">
        {allFilters.categories.map(({ label }) => {
          const isActive = appliedFilters.category.values === label

          return (
            <button
              type="button"
              key={label}
              className={classNames(
                'shrink-0 whitespace-nowrap rounded-full px-4 py-2 text-sm transition',
                isActive ? 'bg-brand-navy font-medium text-white' : 'text-gray-500 hover:text-ink'
              )}
              aria-pressed={isActive}
              onClick={() => onClickCategory(label)}
            >
              {label}
            </button>
          )
        })}
      </div>
    </div>
  )
}
