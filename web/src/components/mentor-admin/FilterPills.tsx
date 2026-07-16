/**
 * Status filter pills for the requests inbox (design 07): active pill is
 * solid navy, idle pills are white with a line border; counts render in
 * Plex Mono.
 */

import classNames from 'classnames'

export interface FilterPillOption<T extends string> {
  value: T
  label: string
  count: number
}

interface FilterPillsProps<T extends string> {
  options: FilterPillOption<T>[]
  value: T
  onChange: (value: T) => void
}

export default function FilterPills<T extends string>({
  options,
  value,
  onChange,
}: FilterPillsProps<T>): JSX.Element {
  return (
    <div className="no-scrollbar flex gap-1.5 overflow-x-auto" role="group" aria-label="Filter by status">
      {options.map((option) => {
        const isActive = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            aria-pressed={isActive}
            className={classNames(
              'flex-none rounded-full text-xs font-semibold transition-colors duration-120',
              isActive
                ? 'bg-brand-navy px-3.5 py-2 text-white'
                : 'border-[1.5px] border-line bg-white px-[13px] py-[6.5px] text-brand-navy hover:border-brand-cobalt/45'
            )}
          >
            {option.label} <span className="font-mono font-bold">· {option.count}</span>
          </button>
        )
      })}
    </div>
  )
}
