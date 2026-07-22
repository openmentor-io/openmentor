import { useState } from 'react'
import classNames from 'classnames'
import Link from 'next/link'
import Image from 'next/image'
import GitHubIcon from '../ui/GitHubIcon'

/**
 * Redesign header (design 01–09): logomark + Archivo wordmark on the left,
 * contextual links + primary CTA on the right, hairline bottom border.
 * Search lives in the homepage hero now, not in the header.
 */

/** Wordmark: "openmentor" navy + ".io" cobalt, Archivo 800 CAPS. */
export function Wordmark({ compact = false }: { compact?: boolean }): JSX.Element {
  return (
    <span
      className={classNames(
        'font-display font-extrabold uppercase tracking-[-0.02em] text-brand-navy',
        compact ? 'text-[15px]' : 'text-[15px] sm:text-xl'
      )}
    >
      openmentor<span className="text-brand-cobalt">.io</span>
    </span>
  )
}

interface NavHeaderProps {
  className?: string
  /**
   * Contextual left-pointing link shown instead of the default nav items
   * (e.g. "← Back to mentors" on the mentor profile page).
   */
  backLink?: { href: string; label: string }
}

export default function NavHeader({ className, backLink }: NavHeaderProps): JSX.Element {
  const [open, setOpen] = useState(false)

  return (
    <header className={classNames('border-b border-line bg-white', className)}>
      <div className="flex items-center justify-between px-5 py-4 sm:px-8 sm:py-5 lg:px-16">
        <Link href="/" className="flex shrink-0 items-center gap-2 sm:gap-3">
          <Image
            src="/brand/logo/svg/logomark.svg"
            width={40}
            height={40}
            alt=""
            unoptimized
            className="h-8 w-8 sm:h-10 sm:w-10"
          />
          <Wordmark />
        </Link>

        {/* desktop nav */}
        <nav className="hidden items-center gap-6 md:flex">
          {backLink ? (
            <Link href={backLink.href} className="text-sm font-semibold text-brand-cobalt">
              ← {backLink.label}
            </Link>
          ) : (
            <>
              <Link
                href="https://github.com/openmentor-io/openmentor"
                target="_blank"
                rel="noopener noreferrer"
                aria-label="OpenMentor on GitHub"
                className="text-ink-soft transition-colors duration-120 hover:text-ink"
              >
                <GitHubIcon className="h-5 w-5" />
              </Link>
              <Link href="/about" className="text-sm font-semibold text-ink hover:text-brand-cobalt">
                About
              </Link>
              <Link href="/donate" className="text-sm font-semibold text-ink hover:text-brand-cobalt">
                Support us
              </Link>
              <Link
                href="/mentor/login"
                className="text-sm font-semibold text-ink hover:text-brand-cobalt"
              >
                Log in
              </Link>
            </>
          )}
          <Link
            href="/bementor"
            className="rounded-btn bg-brand-navy px-5 py-3 text-sm font-bold !text-white transition-colors hover:bg-[#1B3A6E]"
          >
            Become a mentor
          </Link>
        </nav>

        {/* mobile hamburger */}
        <button
          type="button"
          aria-label={open ? 'Close menu' : 'Open menu'}
          aria-expanded={open}
          onClick={() => setOpen(!open)}
          className="flex h-11 w-11 flex-col items-center justify-center gap-[5px] md:hidden"
        >
          <span
            className={classNames(
              'h-0.5 w-5 rounded-full bg-ink transition-transform duration-180',
              open && 'translate-y-[7px] rotate-45'
            )}
          />
          <span
            className={classNames(
              'h-0.5 w-5 rounded-full bg-ink transition-opacity duration-120',
              open && 'opacity-0'
            )}
          />
          <span
            className={classNames(
              'h-0.5 w-5 rounded-full bg-ink transition-transform duration-180',
              open && '-translate-y-[7px] -rotate-45'
            )}
          />
        </button>
      </div>

      {/* mobile menu */}
      {open && (
        <nav className="flex animate-dropdown-in flex-col gap-1 border-t border-line px-5 py-4 md:hidden">
          {backLink && (
            <Link
              href={backLink.href}
              className="rounded-field px-3 py-3 text-sm font-semibold text-brand-cobalt"
              onClick={() => setOpen(false)}
            >
              ← {backLink.label}
            </Link>
          )}
          <Link
            href="https://github.com/openmentor-io/openmentor"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2.5 rounded-field px-3 py-3 text-sm font-semibold text-ink hover:bg-surface"
            onClick={() => setOpen(false)}
          >
            <GitHubIcon className="h-[18px] w-[18px] text-ink-soft" />
            GitHub
          </Link>
          <Link
            href="/about"
            className="rounded-field px-3 py-3 text-sm font-semibold text-ink hover:bg-surface"
            onClick={() => setOpen(false)}
          >
            About
          </Link>
          <Link
            href="/donate"
            className="rounded-field px-3 py-3 text-sm font-semibold text-ink hover:bg-surface"
            onClick={() => setOpen(false)}
          >
            Support us
          </Link>
          <Link
            href="/mentor/login"
            className="rounded-field px-3 py-3 text-sm font-semibold text-ink hover:bg-surface"
            onClick={() => setOpen(false)}
          >
            Log in
          </Link>
          <Link
            href="/bementor"
            className="mt-2 rounded-btn bg-brand-navy px-5 py-3.5 text-center text-sm font-bold !text-white"
            onClick={() => setOpen(false)}
          >
            Become a mentor
          </Link>
        </nav>
      )}
    </header>
  )
}
