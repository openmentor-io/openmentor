import classNames from 'classnames'
import { Menu } from '@headlessui/react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faChevronDown } from '@fortawesome/free-solid-svg-icons'

interface FilterGroupDropdownProps {
  title: string
  values: string[]
  onFilterSelect: (value: string) => void
  allSelectedValues: string[] | string | undefined
  multiSelect?: boolean
}

export default function FilterGroupDropdown({
  title,
  values,
  onFilterSelect,
  allSelectedValues,
  multiSelect = true,
}: FilterGroupDropdownProps): JSX.Element {
  const selectedValuesCount = multiSelect
    ? Array.isArray(allSelectedValues)
      ? allSelectedValues.filter((t) => values.includes(t)).length
      : 0
    : allSelectedValues
    ? 1
    : 0

  const hasSelection = Boolean(selectedValuesCount)

  return (
    <Menu as="div" className="relative inline-block text-left">
      <Menu.Button
        className={classNames(
          'inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-4 py-2 text-sm transition',
          hasSelection ? 'bg-brand-navy font-medium text-white' : 'text-gray-500 hover:text-ink'
        )}
      >
        {title}
        {hasSelection && (
          <span className="inline-flex h-5 min-w-[1.25rem] items-center justify-center rounded-full bg-white/20 px-1 text-xs font-semibold">
            {selectedValuesCount}
          </span>
        )}
        <FontAwesomeIcon icon={faChevronDown} className="h-3 w-3" />
      </Menu.Button>

      <Menu.Items className="absolute left-0 z-20 mt-2 w-max origin-top-left rounded-2xl bg-white p-2 shadow-lg ring-1 ring-black/5 focus:outline-none">
        {values.map((tag) => {
          const isActive = multiSelect
            ? Array.isArray(allSelectedValues) && allSelectedValues.includes(tag)
            : allSelectedValues === tag

          return (
            <Menu.Item key={tag}>
              <div
                className={classNames(
                  'cursor-pointer whitespace-nowrap rounded-full px-4 py-2 text-sm',
                  isActive ? 'bg-brand-navy text-white' : 'text-gray-600 hover:bg-surface'
                )}
                onClick={() => onFilterSelect(tag)}
              >
                {tag}
              </div>
            </Menu.Item>
          )
        })}
      </Menu.Items>
    </Menu>
  )
}
