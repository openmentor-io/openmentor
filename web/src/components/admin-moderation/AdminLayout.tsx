import type { ReactNode } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
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
    <div className="min-h-screen bg-gray-50">
      <header className="border-b border-gray-200 bg-white">
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
              <p className="text-xs uppercase tracking-wide text-gray-500">openmentor.io admin</p>
              <h1 className="text-lg font-semibold text-gray-900">{title}</h1>
            </div>
          </div>
          <div className="flex items-center gap-4">
            <div className="text-right">
              <p className="text-sm font-medium text-gray-800">{session?.name}</p>
              <p className="text-xs text-gray-500">{session?.role}</p>
            </div>
            <button
              onClick={onLogout}
              className="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-100"
            >
              Logout
            </button>
          </div>
        </div>
        <div className="mx-auto flex w-full max-w-7xl gap-3 px-4 pb-4">
          {tabs.map((tab) => {
            const isActive = router.pathname === tab.href
            return (
              <Link
                key={tab.href}
                href={tab.href}
                className={
                  isActive
                    ? 'rounded-md bg-brand-navy px-3 py-2 text-sm font-medium text-white'
                    : 'rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-100'
                }
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
