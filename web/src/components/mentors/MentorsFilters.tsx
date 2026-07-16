import classNames from 'classnames'
import { useEffect, useMemo, useRef, useState } from 'react'
import allFilters from '@/config/filters'
import analytics from '@/lib/analytics'
import type { AppliedFilters, FilterCategory, MentorListItem } from '@/types'

interface MentorsFiltersProps {
  appliedFilters: AppliedFilters
  /** All loaded mentors — used to compute the per-option mono counts. */
  mentors: MentorListItem[]
}

/**
 * Panel identifiers: one shared dropdown panel serves the category pills
 * plus the Experience / Price pills at the end of the row.
 */
type PanelKey = `cat:${string}` | 'experience' | 'price'

const PANEL_WIDTH = 210

/** Chevron glyph from the design (9×6, 1.8 stroke, currentColor). */
function Chevron({ open }: { open: boolean }): JSX.Element {
  return (
    <svg
      width="9"
      height="6"
      viewBox="0 0 9 6"
      fill="none"
      aria-hidden="true"
      className={classNames('transition-transform duration-120', open && 'rotate-180')}
    >
      <path d="M1 1l3.5 3.5L8 1" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

interface PillProps {
  label: string
  active: boolean
  open?: boolean
  hasChevron?: boolean
  badgeCount?: number
  onClick: (target: HTMLElement) => void
}

/**
 * Filter pill: active = navy fill; idle = white with 1.5px cobalt/45%
 * border (design 01 §filter tags).
 */
function Pill({ label, active, open = false, hasChevron = false, badgeCount, onClick }: PillProps) {
  return (
    <button
      type="button"
      className={classNames(
        'flex shrink-0 items-center gap-[7px] whitespace-nowrap rounded-field text-[13px] transition-colors duration-120',
        active
          ? 'bg-brand-navy px-4 py-2.5 font-bold text-white'
          : 'border-[1.5px] border-brand-cobalt/45 bg-white px-[15px] py-[8.5px] font-semibold text-brand-navy hover:bg-brand-cobalt/[0.06]'
      )}
      aria-pressed={active}
      aria-expanded={hasChevron ? open : undefined}
      onClick={(event) => onClick(event.currentTarget)}
    >
      {label}
      {badgeCount !== undefined && badgeCount > 0 && (
        <span
          className={classNames(
            'flex h-[18px] min-w-[18px] items-center justify-center rounded-full px-1 font-mono text-[10px] font-semibold',
            active ? 'bg-white/20 text-white' : 'bg-brand-cobalt/10 text-brand-navy'
          )}
        >
          {badgeCount}
        </span>
      )}
      {hasChevron && <Chevron open={open} />}
    </button>
  )
}

interface PanelItemProps {
  label: string
  count: number
  selected: boolean
  onClick: () => void
}

/** Dropdown panel row: label left, mono count right; selected = cobalt fill. */
function PanelItem({ label, count, selected, onClick }: PanelItemProps): JSX.Element {
  return (
    <button
      type="button"
      className={classNames(
        'flex items-center justify-between gap-3 rounded-[9px] px-3 py-[9px] text-left text-[13px]',
        selected
          ? 'bg-brand-cobalt font-semibold text-white'
          : 'font-medium text-ink hover:bg-surface'
      )}
      aria-pressed={selected}
      onClick={onClick}
    >
      <span>{label}</span>
      <span
        className={classNames(
          'font-mono text-[11px] font-medium',
          selected ? 'text-white/90' : 'text-ink-mute'
        )}
      >
        {count.toLocaleString('en-US')}
      </span>
    </button>
  )
}

/**
 * Catalog filter row (design 01): "All mentors" + two-level category pills
 * (category filters by ALL its tags; the dropdown narrows to a single tag),
 * then Experience (multi-select) and Price (single-select) dropdown pills.
 * The row scrolls horizontally edge-to-edge on mobile; dropdowns render as
 * absolute panels anchored under the clicked pill (rendered as siblings of
 * the scroll row so overflow-x never clips them).
 */
export default function MentorsFilters({ appliedFilters, mentors }: MentorsFiltersProps) {
  const [openPanel, setOpenPanel] = useState<PanelKey | null>(null)
  const [panelLeft, setPanelLeft] = useState(0)
  const wrapperRef = useRef<HTMLDivElement>(null)

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

  // Close the open panel on outside click / Escape.
  useEffect(() => {
    if (!openPanel) return

    const onPointerDown = (event: MouseEvent | TouchEvent): void => {
      if (wrapperRef.current && !wrapperRef.current.contains(event.target as Node)) {
        setOpenPanel(null)
      }
    }
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') setOpenPanel(null)
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('touchstart', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('touchstart', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [openPanel])

  // Per-option counts, computed from the loaded mentors.
  const tagCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const mentor of mentors) {
      for (const tag of mentor.tags) {
        counts.set(tag, (counts.get(tag) ?? 0) + 1)
      }
    }
    return counts
  }, [mentors])

  const experienceCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const [label, value] of Object.entries(allFilters.experience)) {
      counts.set(label, mentors.filter((mentor) => mentor.experience === value).length)
    }
    return counts
  }, [mentors])

  const priceCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const [label, matches] of Object.entries(allFilters.byPrice)) {
      counts.set(label, mentors.filter((mentor) => matches(mentor.price)).length)
    }
    return counts
  }, [mentors])

  const trackFilterChange = (filterType: string, value: string, added: boolean): void => {
    analytics.event(analytics.events.MENTOR_FILTER_CHANGED, {
      filter_type: filterType,
      filter_value: value,
      action: added ? 'added' : 'removed',
    })
  }

  /** Open a panel anchored under the clicked pill (or close it if open). */
  const togglePanel = (key: PanelKey, target: HTMLElement): void => {
    if (openPanel === key) {
      setOpenPanel(null)
      return
    }

    const wrapper = wrapperRef.current
    if (wrapper) {
      const wrapperRect = wrapper.getBoundingClientRect()
      const pillRect = target.getBoundingClientRect()
      const maxLeft = Math.max(0, wrapperRect.width - PANEL_WIDTH)
      setPanelLeft(Math.max(0, Math.min(pillRect.left - wrapperRect.left, maxLeft)))
    }
    setOpenPanel(key)
  }

  const topicFilterActive =
    Boolean(appliedFilters.category.values) || appliedFilters.tags.values.length > 0

  const onClickAll = (): void => {
    if (topicFilterActive) {
      analytics.event(analytics.events.MENTOR_FILTERS_RESET, { scope: 'topic' })
    }
    appliedFilters.category.reset()
    appliedFilters.tags.set([])
    setOpenPanel(null)
  }

  const onClickCategory = (category: FilterCategory, target: HTMLElement): void => {
    const isActive = appliedFilters.category.values === category.label
    const hasDropdown = category.tags.length > 1

    if (!isActive) {
      appliedFilters.category.set(category.label)
      appliedFilters.tags.set([])
      trackFilterChange('category', category.label, true)

      if (hasDropdown) {
        togglePanel(`cat:${category.label}`, target)
      } else {
        setOpenPanel(null)
      }
      return
    }

    if (hasDropdown) {
      // Clicking the active category again toggles its dropdown.
      togglePanel(`cat:${category.label}`, target)
    } else {
      // Single-tag category: toggle back to "All".
      appliedFilters.category.reset()
      appliedFilters.tags.set([])
      trackFilterChange('category', category.label, false)
    }
  }

  const onClickTag = (tag: string): void => {
    const isSelected = appliedFilters.tags.values.includes(tag)
    // Single tag narrows the category; re-picking it widens back to the
    // whole category (the category filter itself stays on).
    appliedFilters.tags.set(isSelected ? [] : [tag])
    trackFilterChange('tag', tag, !isSelected)
    setOpenPanel(null)
  }

  const onClickExperience = (label: string): void => {
    const values = appliedFilters.experience.values
    const isSelected = values.includes(label)
    appliedFilters.experience.set(
      isSelected ? values.filter((value) => value !== label) : [...values, label]
    )
    trackFilterChange('experience', label, !isSelected)
    // Multi-select: the panel stays open.
  }

  const onClickPrice = (label: string): void => {
    const isSelected = appliedFilters.price.values === label
    appliedFilters.price.set(isSelected ? undefined : label)
    trackFilterChange('price', label, !isSelected)
    setOpenPanel(null)
  }

  const openCategory = openPanel?.startsWith('cat:')
    ? allFilters.categories.find((category) => `cat:${category.label}` === openPanel)
    : undefined

  return (
    <div ref={wrapperRef} className="relative">
      {/* Mobile: full-bleed horizontal scroll (sections use px-5/px-8),
          hidden scrollbar; desktop wraps. */}
      <div className="no-scrollbar -mx-5 flex items-center gap-2 overflow-x-auto px-5 py-0.5 sm:-mx-8 sm:px-8 lg:mx-0 lg:flex-wrap lg:overflow-x-visible lg:px-0">
        <Pill label="All mentors" active={!topicFilterActive} onClick={onClickAll} />

        {allFilters.categories.map((category) => {
          const isActive = appliedFilters.category.values === category.label
          const hasDropdown = category.tags.length > 1
          const selectedTag = category.tags.find((tag) => appliedFilters.tags.values.includes(tag))

          return (
            <Pill
              key={category.label}
              label={selectedTag ?? category.label}
              active={isActive}
              hasChevron={hasDropdown}
              open={openPanel === `cat:${category.label}`}
              onClick={(target) => onClickCategory(category, target)}
            />
          )
        })}

        <Pill
          label="Experience"
          active={appliedFilters.experience.values.length > 0}
          hasChevron
          open={openPanel === 'experience'}
          badgeCount={appliedFilters.experience.values.length}
          onClick={(target) => togglePanel('experience', target)}
        />

        <Pill
          label="Price"
          active={Boolean(appliedFilters.price.values)}
          hasChevron
          open={openPanel === 'price'}
          badgeCount={appliedFilters.price.values ? 1 : 0}
          onClick={(target) => togglePanel('price', target)}
        />
      </div>

      {openPanel && (
        <div
          className="absolute top-full z-20 mt-2 flex animate-dropdown-in flex-col gap-0.5 rounded-card border border-line bg-white p-2 shadow-dropdown"
          style={{ left: panelLeft, width: PANEL_WIDTH }}
        >
          {openCategory &&
            openCategory.tags.map((tag) => (
              <PanelItem
                key={tag}
                label={tag}
                count={tagCounts.get(tag) ?? 0}
                selected={appliedFilters.tags.values.includes(tag)}
                onClick={() => onClickTag(tag)}
              />
            ))}

          {openPanel === 'experience' &&
            Object.keys(allFilters.experience).map((label) => (
              <PanelItem
                key={label}
                label={label}
                count={experienceCounts.get(label) ?? 0}
                selected={appliedFilters.experience.values.includes(label)}
                onClick={() => onClickExperience(label)}
              />
            ))}

          {openPanel === 'price' &&
            Object.keys(allFilters.byPrice).map((label) => (
              <PanelItem
                key={label}
                label={label}
                count={priceCounts.get(label) ?? 0}
                selected={appliedFilters.price.values === label}
                onClick={() => onClickPrice(label)}
              />
            ))}
        </div>
      )}
    </div>
  )
}
