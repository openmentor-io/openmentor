import { useEffect, useMemo, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import type { MentorModerationFilter, AdminMentorListItem } from '@/types'
import { useAdminAuth } from './AdminAuthContext'
import { AdminLayout } from './AdminLayout'
import { moderationStatusBadgeClass } from './utils'
import { getModerationMentors } from '@/lib/admin-moderation-api'

const PAGE_SIZE = 50

interface MentorModerationListPageProps {
  status: MentorModerationFilter
  title: string
}

export function MentorModerationListPage({
  status,
  title,
}: MentorModerationListPageProps): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading, session } = useAdminAuth()
  const [mentors, setMentors] = useState<AdminMentorListItem[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [page, setPage] = useState(1)

  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/admin/login')
      return
    }

    if (session?.role === 'moderator' && status !== 'pending') {
      router.replace('/admin/mentors/pending')
    }
  }, [authLoading, isAuthenticated, router, session, status])

  useEffect(() => {
    if (!isAuthenticated || !session) return
    let mounted = true

    const loadMentors = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const data = await getModerationMentors(status)
        if (mounted) {
          setMentors(data)
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : 'Failed to load mentors')
        }
      } finally {
        if (mounted) {
          setIsLoading(false)
        }
      }
    }

    loadMentors()
    return () => {
      mounted = false
    }
  }, [isAuthenticated, session, status])

  const filteredMentors = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()
    const sorted = [...mentors].sort((a, b) =>
      status === 'pending'
        ? new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
        : new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    )
    if (!query) return sorted

    return sorted.filter((mentor) => {
      const name = mentor.name.toLowerCase()
      const email = mentor.email.toLowerCase()
      const contact = mentor.contact.toLowerCase()
      return name.includes(query) || email.includes(query) || contact.includes(query)
    })
  }, [mentors, searchQuery, status])

  const totalPages = Math.max(1, Math.ceil(filteredMentors.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const pageItems = filteredMentors.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  useEffect(() => {
    setPage(1)
  }, [searchQuery, mentors.length])

  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <AdminLayout title={title}>
      <div className="mb-4">
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Search by name, email, contact"
          className="field"
        />
      </div>

      <div className="meta-mono mb-4 text-ink-mute">
        Showing {pageItems.length} of {filteredMentors.length}
      </div>

      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <FontAwesomeIcon
            icon={faCircleNotch}
            className="animate-spin text-2xl text-brand-cobalt"
          />
        </div>
      )}

      {error && !isLoading && (
        <div className="rounded-card border border-danger/40 bg-white p-4 text-sm font-medium text-danger">
          {error}
        </div>
      )}

      {!isLoading && !error && pageItems.length === 0 && (
        <div className="rounded-card border border-line bg-white p-6 text-sm text-ink-soft">
          No mentors found.
        </div>
      )}

      {!isLoading && !error && pageItems.length > 0 && (
        <div className="space-y-2.5">
          {pageItems.map((mentor) => (
            <Link
              key={mentor.mentorId}
              href={`/admin/mentors/${mentor.mentorId}`}
              className="block rounded-card border border-line bg-white p-4 transition-all duration-180 hover:shadow-card-hover"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <p className="my-0 font-name text-base font-bold tracking-[-0.015em] text-ink">
                    {mentor.name}
                  </p>
                  <p className="my-0 text-sm text-ink-soft">{mentor.email}</p>
                  <p className="my-0 text-sm text-ink-mute">{mentor.contact}</p>
                </div>
                <span className={moderationStatusBadgeClass(mentor.status)}>{mentor.status}</span>
              </div>
              <div className="mt-3 text-sm text-ink">
                <p className="my-0">
                  {mentor.job} {mentor.workplace ? `• ${mentor.workplace}` : ''}
                </p>
                <p className="meta-mono my-0 mt-1 text-ink-mute">{mentor.price}</p>
              </div>
            </Link>
          ))}
        </div>
      )}

      {!isLoading && !error && filteredMentors.length > PAGE_SIZE && (
        <div className="mt-6 flex items-center justify-between">
          <button
            type="button"
            onClick={() => setPage((prev) => Math.max(1, prev - 1))}
            disabled={currentPage <= 1}
            className="button-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            Previous
          </button>
          <span className="meta-mono text-ink-mute">
            Page {currentPage} of {totalPages}
          </span>
          <button
            type="button"
            onClick={() => setPage((prev) => Math.min(totalPages, prev + 1))}
            disabled={currentPage >= totalPages}
            className="button-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            Next
          </button>
        </div>
      )}
    </AdminLayout>
  )
}
