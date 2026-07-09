import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faSearch } from '@fortawesome/free-solid-svg-icons'
import type { ChangeEvent } from 'react'

interface MentorsSearchProps {
  value: string
  onChange: (value: string) => void
}

export default function MentorsSearch({ value, onChange }: MentorsSearchProps): JSX.Element {
  return (
    <div className="relative w-full">
      <div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
        <FontAwesomeIcon className="text-gray-400" icon={faSearch} fixedWidth />
      </div>

      <input
        type="search"
        className="w-full rounded-full border-0 bg-surface py-2.5 pl-11 pr-4 text-sm text-ink placeholder-gray-400 focus:ring-2 focus:ring-ink/10"
        placeholder="Search by role, skill, company, name…"
        autoComplete="off"
        value={value}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
      />
    </div>
  )
}
