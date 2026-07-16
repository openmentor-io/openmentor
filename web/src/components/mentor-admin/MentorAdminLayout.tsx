/**
 * Layout component for Mentor Admin pages
 *
 * Dashboard shell per designs 07–09: fixed white sidebar on desktop
 * (logomark + wordmark, nav items, user block with log out) and a compact
 * top bar with a collapsible menu on mobile.
 */

import { useState } from 'react'
import type { ReactNode } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
import classNames from 'classnames'
import { useMentorAuth } from './MentorAuthContext'
import { nameInitials } from './utils'

interface MentorAdminLayoutProps {
  children: ReactNode
  title?: string
  /** Rendered on the H1 row, right-aligned (e.g. the status filter pills). */
  actions?: ReactNode
}

interface NavItemProps {
  href: string
  label: string
  isActive: boolean
  onClick?: () => void
}

function NavItem({ href, label, isActive, onClick }: NavItemProps): JSX.Element {
  return (
    <Link
      href={href}
      onClick={onClick}
      className={classNames(
        'block rounded-[11px] px-3.5 py-3 text-sm transition-colors duration-120',
        isActive ? 'bg-brand-navy font-semibold text-white' : 'font-medium text-ink-mute hover:bg-surface'
      )}
    >
      {label}
    </Link>
  )
}

export default function MentorAdminLayout({
  children,
  title,
  actions,
}: MentorAdminLayoutProps): JSX.Element {
  const router = useRouter()
  const { session, logout } = useMentorAuth()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [isLoggingOut, setIsLoggingOut] = useState(false)

  const handleLogout = async (): Promise<void> => {
    setIsLoggingOut(true)
    try {
      await logout()
      router.push('/mentor/login')
    } finally {
      setIsLoggingOut(false)
    }
  }

  const isActive = (path: string): boolean => {
    if (path === '/mentor') {
      return router.pathname === '/mentor' || router.pathname.startsWith('/mentor/requests')
    }
    return router.pathname.startsWith(path)
  }

  const navItems = [
    { href: '/mentor', label: 'Requests' },
    { href: '/mentor/past', label: 'Archive' },
    { href: '/mentor/profile/edit', label: 'My profile' },
  ]

  const logoutButton = (
    <button
      onClick={handleLogout}
      disabled={isLoggingOut}
      className="text-left text-[11px] text-ink-soft transition-colors duration-120 hover:text-ink disabled:opacity-50"
    >
      {isLoggingOut ? 'Signing out...' : 'Log out'}
    </button>
  )

  return (
    <div className="flex min-h-screen bg-surface">
      {/* Desktop sidebar */}
      <aside className="hidden w-60 flex-none flex-col gap-0.5 border-r border-line bg-white px-4 py-6 md:flex">
        <Link href="/" className="flex items-center gap-2.5 px-2.5 pb-6">
          <Image
            src="/brand/logo/svg/logomark.svg"
            width={34}
            height={34}
            alt=""
            unoptimized
          />
          <span className="font-display text-[15px] font-extrabold uppercase tracking-[-0.02em] text-brand-navy">
            openmentor
          </span>
        </Link>

        <nav className="flex flex-col gap-0.5" aria-label="Dashboard">
          {navItems.map((item) => (
            <NavItem
              key={item.href}
              href={item.href}
              label={item.label}
              isActive={isActive(item.href)}
            />
          ))}
        </nav>

        <div className="flex-1" />

        <div className="flex items-center gap-2.5 border-t border-line pl-2.5 pt-3.5">
          <div
            aria-hidden="true"
            className="flex h-[34px] w-[34px] flex-none items-center justify-center rounded-full bg-brand-navy font-name text-xs font-bold text-white"
          >
            {session ? nameInitials(session.name) : ''}
          </div>
          <div className="min-w-0">
            <div className="truncate text-[13px] font-semibold text-ink">{session?.name}</div>
            {logoutButton}
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile top bar */}
        <header className="flex items-center justify-between border-b border-line bg-white px-5 py-3.5 md:hidden">
          <Link href="/">
            <Image src="/brand/logo/svg/logomark.svg" width={30} height={30} alt="openmentor.io" unoptimized />
          </Link>
          {title && (
            <span className="font-display text-sm font-extrabold uppercase text-ink">{title}</span>
          )}
          <button
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            className="flex h-8 w-8 items-center justify-center rounded-full bg-brand-navy font-name text-xs font-bold text-white"
            aria-label="Menu"
            aria-expanded={mobileMenuOpen}
          >
            {session ? nameInitials(session.name) : '☰'}
          </button>
        </header>

        {/* Mobile menu */}
        {mobileMenuOpen && (
          <div className="animate-dropdown-in border-b border-line bg-white px-5 py-3 md:hidden">
            <nav className="flex flex-col gap-0.5" aria-label="Dashboard">
              {navItems.map((item) => (
                <NavItem
                  key={item.href}
                  href={item.href}
                  label={item.label}
                  isActive={isActive(item.href)}
                  onClick={() => setMobileMenuOpen(false)}
                />
              ))}
            </nav>
            <div className="mt-2 border-t border-line px-3.5 pt-3">{logoutButton}</div>
          </div>
        )}

        {/* Main content */}
        <main className="mx-auto w-full max-w-[900px] flex-1 px-4 py-6 sm:px-8 sm:py-8">
          {(title || actions) && (
            <div className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
              {title && (
                <h1 className="hidden text-2xl tracking-[-0.02em] sm:text-[28px] md:block">
                  {title}
                </h1>
              )}
              {actions}
            </div>
          )}
          {children}
        </main>
      </div>
    </div>
  )
}
