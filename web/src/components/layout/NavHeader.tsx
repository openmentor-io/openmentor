import { useState } from 'react'
import classNames from 'classnames'
import Link from 'next/link'
import Image from 'next/image'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faSearch } from '@fortawesome/free-solid-svg-icons'
import MentorsSearch from '../mentors/MentorsSearch'
import styles from './NavHeader.module.css'

function Nav(): JSX.Element {
  return (
    <ul>
      <li>
        <Link href="https://blog.openmentor.io">Blog</Link>
      </li>
      <li>
        <Link href="/donate">Support us</Link>
      </li>
      <li>
        <Link href="/mentor/login">Log in</Link>
      </li>
      <li>
        <Link
          href="/bementor"
          className="inline-block rounded-full bg-brand-navy px-4 py-2 text-sm font-medium !text-white !opacity-100 transition hover:bg-brand-navy/90"
        >
          Become a mentor
        </Link>
      </li>
    </ul>
  )
}

interface NavHeaderProps {
  className?: string
  /** When provided (homepage), the header search drives the mentors search. */
  searchValue?: string
  onSearchChange?: (value: string) => void
}

export default function NavHeader({
  className,
  searchValue,
  onSearchChange,
}: NavHeaderProps): JSX.Element {
  const [open, setOpen] = useState(false)

  return (
    <div className={classNames(styles.container, 'bg-white', className)}>
      <div className="container flex items-center gap-4">
        <Link href="/" className="flex shrink-0 items-center pt-1">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={117}
            height={32}
            alt="openmentor.io"
            unoptimized
          />
        </Link>

        <div className="mx-auto hidden w-full max-w-xl md:block">
          {onSearchChange ? (
            <MentorsSearch value={searchValue ?? ''} onChange={onSearchChange} />
          ) : (
            <Link
              href="/#list"
              className="flex w-full items-center gap-3 rounded-full bg-surface py-2.5 pl-4 pr-4 text-sm text-gray-400"
            >
              <FontAwesomeIcon icon={faSearch} fixedWidth />
              Search mentors
            </Link>
          )}
        </div>

        <div className={classNames(styles.toggle, 'md:hidden')} onClick={() => setOpen(!open)}>
          ☰
        </div>
        <div className={classNames(styles.mobile, open ? styles.active : '')}>
          <Nav />
        </div>
        <div
          className={classNames(styles.overlay, open ? 'block' : 'hidden')}
          onClick={() => setOpen(!open)}
        ></div>

        <nav className={classNames(styles.desktop, 'hidden shrink-0 md:block')}>
          <Nav />
        </nav>
      </div>

      {onSearchChange && (
        <div className="container mt-1 pb-3 md:hidden">
          <MentorsSearch value={searchValue ?? ''} onChange={onSearchChange} />
        </div>
      )}
    </div>
  )
}
