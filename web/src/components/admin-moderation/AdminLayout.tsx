import type { ReactNode } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
import classNames from 'classnames'
import { useAdminAuth } from './AdminAuthContext'

interface AdminLayoutProps {
  title: string
  children: ReactNode
}

interface TabItem {
  href: string
  label: string
}

export function AdminLayout({ title, children }: AdminLayoutProps): JSX.Element {
  const router = useRouter()
  const { session, logout } = useAdminAuth()

  const tabs: TabItem[] = [{ href: '/admin/mentors/pending', label: 'Pending' }]

  if (session?.role === 'admin') {
    tabs.push({ href: '/admin/mentors/approved', label: 'Approved' })
    tabs.push({ href: '/admin/mentors/declined', label: 'Declined' })
  }

  const onLogout = async (): Promise<void> => {
    await logout()
    router.replace('/admin/login')
  }

  return (
    <div className="min-h-screen bg-surface">
      <header className="border-b border-line bg-white">
        <div className="mx-auto flex w-full max-w-7xl items-center justify-between px-4 py-4">
          <div className="flex items-center gap-3">
            <Image
              src="/brand/logo/svg/logomark.svg"
              width={28}
              height={28}
              alt="openmentor.io"
              unoptimized
            />
            <div>
              <p className="meta-mono my-0 text-ink-mute">openmentor.io admin</p>
              <h1 className="text-lg tracking-[-0.02em] text-ink">{title}</h1>
            </div>
          </div>
          <div className="flex items-center gap-4">
            <div className="text-right">
              <p className="my-0 text-sm font-semibold text-ink">{session?.name}</p>
              <p className="meta-mono my-0 text-ink-mute">{session?.role}</p>
            </div>
            <button onClick={onLogout} className="button-ghost px-3 py-2 text-sm">
              Log out
            </button>
          </div>
        </div>
        <div className="mx-auto flex w-full max-w-7xl gap-1.5 px-4 pb-4">
          {tabs.map((tab) => {
            const isActive = router.pathname === tab.href
            return (
              <Link
                key={tab.href}
                href={tab.href}
                className={classNames(
                  'rounded-full text-xs font-semibold transition-colors duration-120',
                  isActive
                    ? 'bg-brand-navy px-3.5 py-2 text-white'
                    : 'border-[1.5px] border-line bg-white px-[13px] py-[6.5px] text-brand-navy hover:border-brand-cobalt/45'
                )}
              >
                {tab.label}
              </Link>
            )
          })}
        </div>
      </header>
      <main className="mx-auto w-full max-w-7xl px-4 py-6">{children}</main>
    </div>
  )
}
