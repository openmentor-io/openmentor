/**
 * Mentor Active Requests Page (design 07 — requests inbox)
 *
 * Displays pending, contacted, and working requests with status filter
 * pills, search, sort, shimmer loading rows and the design empty state.
 */

import { useState, useEffect, useMemo } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import type { MentorClientRequest, RequestStatus, SortOrder } from '@/types'
import { ACTIVE_STATUSES, STATUS_LABELS } from '@/types'
import {
  MentorAuthProvider,
  useMentorAuth,
  MentorAdminLayout,
  RequestCard,
  RequestListSkeleton,
  FilterPills,
  SearchInput,
  SortToggle,
} from '@/components/mentor-admin'
import { getActiveRequests } from '@/lib/mentor-admin-api'

type StatusFilter = 'all' | RequestStatus

/**
 * Filter requests by search query
 */
function filterRequests(requests: MentorClientRequest[], query: string): MentorClientRequest[] {
  if (!query.trim()) return requests

  const lowerQuery = query.toLowerCase()
  return requests.filter(
    (r) =>
      r.name.toLowerCase().includes(lowerQuery) ||
      r.email.toLowerCase().includes(lowerQuery) ||
      r.contact.toLowerCase().includes(lowerQuery) ||
      r.details.toLowerCase().includes(lowerQuery) ||
      r.id.toLowerCase().includes(lowerQuery)
  )
}

/**
 * Sort requests by creation date
 */
function sortRequests(requests: MentorClientRequest[], order: SortOrder): MentorClientRequest[] {
  return [...requests].sort((a, b) => {
    const dateA = new Date(a.createdAt).getTime()
    const dateB = new Date(b.createdAt).getTime()
    return order === 'newest' ? dateB - dateA : dateA - dateB
  })
}

function ActiveRequestsContent(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading } = useMentorAuth()
  const [requests, setRequests] = useState<MentorClientRequest[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [sortOrder, setSortOrder] = useState<SortOrder>('newest')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/mentor/login')
    }
  }, [authLoading, isAuthenticated, router])

  // Load requests
  useEffect(() => {
    if (!isAuthenticated) return

    const loadRequests = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const data = await getActiveRequests()
        setRequests(data)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load requests')
      } finally {
        setIsLoading(false)
      }
    }

    loadRequests()
  }, [isAuthenticated])

  // Status filter pill options with counts
  const pillOptions = useMemo(
    () => [
      { value: 'all' as StatusFilter, label: 'All', count: requests.length },
      ...ACTIVE_STATUSES.map((status) => ({
        value: status as StatusFilter,
        label: STATUS_LABELS[status],
        count: requests.filter((r) => r.status === status).length,
      })),
    ],
    [requests]
  )

  // Filter and sort requests
  const filteredRequests = useMemo(() => {
    let result = filterRequests(requests, searchQuery)
    if (statusFilter !== 'all') {
      result = result.filter((r) => r.status === statusFilter)
    }
    return sortRequests(result, sortOrder)
  }, [requests, searchQuery, sortOrder, statusFilter])

  // Show loading while checking auth
  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <>
      <Head>
        <title>Requests — openmentor.io</title>
      </Head>

      <MentorAdminLayout
        title="Requests"
        actions={
          !isLoading && !error && requests.length > 0 ? (
            <FilterPills options={pillOptions} value={statusFilter} onChange={setStatusFilter} />
          ) : undefined
        }
      >
        {/* Search and Sort */}
        {!isLoading && !error && requests.length > 0 && (
          <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="max-w-md flex-1">
              <SearchInput
                value={searchQuery}
                onChange={setSearchQuery}
                placeholder="Search by name, email, contact..."
              />
            </div>
            <SortToggle value={sortOrder} onChange={setSortOrder} />
          </div>
        )}

        {/* Loading state — shimmer rows */}
        {isLoading && <RequestListSkeleton />}

        {/* Error state */}
        {error && !isLoading && (
          <div className="rounded-card border border-danger/40 bg-white p-5">
            <p className="my-0 text-sm font-medium text-danger">{error}</p>
            <button
              onClick={() => window.location.reload()}
              className="mt-2 text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              Try again
            </button>
          </div>
        )}

        {/* Empty state (design 07) */}
        {!isLoading && !error && requests.length === 0 && (
          <div className="flex animate-rise-in flex-col items-center rounded-panel border border-line bg-white px-10 py-16 text-center">
            <div aria-hidden="true" className="relative h-[88px] w-[88px]">
              <div className="m-[9px] h-[70px] w-[70px] rounded-full border-[9px] border-line" />
              <div className="absolute -left-3.5 bottom-3 h-[9px] w-10 -rotate-[35deg] rounded-full bg-brand-cobalt opacity-35" />
            </div>
            <h2 className="mt-5 text-[22px] tracking-[-0.01em] text-ink">No requests yet</h2>
            <p className="my-0 mt-2.5 max-w-[400px] text-sm leading-relaxed text-ink-soft">
              Most mentors get their first request within two weeks — complete profiles with a
              photo get them noticeably faster.
            </p>
            <div className="mt-6 flex flex-wrap justify-center gap-2.5">
              <Link href="/mentor/profile/edit" className="button">
                Improve my profile
              </Link>
              <Link href="/mentor/past" className="button-secondary">
                View the archive
              </Link>
            </div>
          </div>
        )}

        {/* No search / filter results */}
        {!isLoading && !error && requests.length > 0 && filteredRequests.length === 0 && (
          <div className="rounded-panel border border-line bg-white py-12 text-center">
            <p className="my-0 text-sm text-ink-soft">
              {searchQuery ? `Nothing found for "${searchQuery}"` : 'Nothing here with this status'}
            </p>
            <button
              onClick={() => {
                setSearchQuery('')
                setStatusFilter('all')
              }}
              className="mt-2 text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              Clear filters
            </button>
          </div>
        )}

        {/* Requests list */}
        {!isLoading && !error && filteredRequests.length > 0 && (
          <div className="flex flex-col gap-2.5">
            {filteredRequests.map((request, index) => (
              <div
                key={request.id}
                className="animate-rise-in"
                style={{ animationDelay: `${Math.min(index, 12) * 30}ms` }}
              >
                <RequestCard request={request} />
              </div>
            ))}
          </div>
        )}
      </MentorAdminLayout>
    </>
  )
}

export default function MentorIndexPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <ActiveRequestsContent />
    </MentorAuthProvider>
  )
}
