import type { ChangeEvent } from 'react'

interface MentorsSearchProps {
  value: string
  onChange: (value: string) => void
}

/**
 * Hero search field (design 01): surface bg, 1.5px line border, 14px
 * radius, inline search glyph. Focus swaps the border to cobalt and adds
 * the soft focus ring (120ms).
 */
export default function MentorsSearch({ value, onChange }: MentorsSearchProps): JSX.Element {
  return (
    <div className="relative w-full">
      <svg
        className="pointer-events-none absolute left-[18px] top-1/2 -translate-y-1/2"
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="none"
        aria-hidden="true"
      >
        <circle cx="7" cy="7" r="5" stroke="#5B6270" strokeWidth="2" />
        <path d="M11 11l3.5 3.5" stroke="#5B6270" strokeWidth="2" strokeLinecap="round" />
      </svg>

      <input
        type="search"
        className="w-full rounded-card border-[1.5px] border-line bg-surface py-[13px] pl-11 pr-4 text-[15px] font-medium text-ink transition-[border-color,box-shadow] duration-120 placeholder:text-ink-soft focus:border-brand-cobalt focus:shadow-focus-field focus:outline-none focus:ring-0"
        placeholder="Role, skill, or company…"
        aria-label="Search mentors by role, skill, or company"
        autoComplete="off"
        value={value}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
      />
    </div>
  )
}
