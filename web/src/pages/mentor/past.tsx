/**
 * Mentor Past Requests Page (design 07 — archive view)
 *
 * Displays done, declined, and unavailable requests with status filter
 * pills, search, sort, a with-review toggle and pagination.
 */

import { useState, useEffect, useMemo } from 'react'
import Head from 'next/head'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import classNames from 'classnames'
import type { MentorClientRequest, RequestStatus, SortOrder } from '@/types'
import { PAST_STATUSES, STATUS_LABELS } from '@/types'
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
import { getPastRequests } from '@/lib/mentor-admin-api'

const PAGE_SIZE = 20

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

function PastRequestsContent(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading } = useMentorAuth()
  const [requests, setRequests] = useState<MentorClientRequest[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [sortOrder, setSortOrder] = useState<SortOrder>('newest')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [showOnlyWithReview, setShowOnlyWithReview] = useState(false)
  const [displayCount, setDisplayCount] = useState(PAGE_SIZE)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/mentor/login')
    }
  }, [authLoading, isAuthenticated, router])

  // Load requests when page is mounted (lazy loading as per spec)
  useEffect(() => {
    if (!isAuthenticated) return

    const loadRequests = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const data = await getPastRequests()
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
      ...PAST_STATUSES.map((status) => ({
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
    if (showOnlyWithReview) {
      result = result.filter((r) => r.review && r.review.trim() !== '')
    }
    return sortRequests(result, sortOrder)
  }, [requests, searchQuery, sortOrder, statusFilter, showOnlyWithReview])

  // Paginated requests
  const displayedRequests = useMemo(
    () => filteredRequests.slice(0, displayCount),
    [filteredRequests, displayCount]
  )

  const hasMore = displayCount < filteredRequests.length

  const loadMore = (): void => {
    setDisplayCount((prev) => Math.min(prev + PAGE_SIZE, filteredRequests.length))
  }

  // Reset pagination when search, sort, or filter changes
  useEffect(() => {
    setDisplayCount(PAGE_SIZE)
  }, [searchQuery, sortOrder, statusFilter, showOnlyWithReview])

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
        <title>Archive — openmentor.io</title>
      </Head>

      <MentorAdminLayout
        title="Archive"
        actions={
          !isLoading && !error && requests.length > 0 ? (
            <FilterPills options={pillOptions} value={statusFilter} onChange={setStatusFilter} />
          ) : undefined
        }
      >
        {/* Search, Sort, and Filter */}
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
            <label className="inline-flex cursor-pointer items-center">
              <input
                type="checkbox"
                checked={showOnlyWithReview}
                onChange={(e) => setShowOnlyWithReview(e.target.checked)}
                className="peer sr-only"
              />
              <span
                aria-hidden="true"
                className={classNames(
                  'relative h-6 w-11 rounded-full transition-colors duration-180 ease-out peer-focus-visible:shadow-focus-ring',
                  showOnlyWithReview ? 'bg-brand-mint' : 'bg-line'
                )}
              >
                <span
                  className={classNames(
                    'absolute top-[2px] h-5 w-5 rounded-full bg-white shadow transition-all duration-180 ease-out',
                    showOnlyWithReview ? 'left-[22px]' : 'left-[2px]'
                  )}
                />
              </span>
              <span className="ms-3 text-sm font-medium text-ink">With a review</span>
            </label>
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

        {/* Empty state */}
        {!isLoading && !error && requests.length === 0 && (
          <div className="flex animate-rise-in flex-col items-center rounded-panel border border-line bg-white px-10 py-16 text-center">
            <div aria-hidden="true" className="relative h-[88px] w-[88px]">
              <div className="m-[9px] h-[70px] w-[70px] rounded-full border-[9px] border-line" />
            </div>
            <h2 className="mt-5 text-[22px] tracking-[-0.01em] text-ink">The archive is empty</h2>
            <p className="my-0 mt-2.5 max-w-[400px] text-sm leading-relaxed text-ink-soft">
              Completed and declined requests will appear here.
            </p>
          </div>
        )}

        {/* No search / filter results */}
        {!isLoading && !error && requests.length > 0 && filteredRequests.length === 0 && (
          <div className="rounded-panel border border-line bg-white py-12 text-center">
            <p className="my-0 text-sm text-ink-soft">
              {searchQuery ? `Nothing found for "${searchQuery}"` : 'Nothing here with this filter'}
            </p>
            <button
              onClick={() => {
                setSearchQuery('')
                setStatusFilter('all')
                setShowOnlyWithReview(false)
              }}
              className="mt-2 text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              Clear filters
            </button>
          </div>
        )}

        {/* Requests list */}
        {!isLoading && !error && displayedRequests.length > 0 && (
          <>
            <div className="flex flex-col gap-2.5">
              {displayedRequests.map((request, index) => (
                <div
                  key={request.id}
                  className="animate-rise-in"
                  style={{ animationDelay: `${Math.min(index, 12) * 30}ms` }}
                >
                  <RequestCard request={request} />
                </div>
              ))}
            </div>

            {/* Pagination info and load more */}
            <div className="mt-6 text-center">
              <p className="meta-mono my-0 mb-3 text-ink-mute">
                Showing {displayedRequests.length} of {filteredRequests.length}
              </p>
              {hasMore && (
                <button onClick={loadMore} className="button-secondary">
                  Show more
                </button>
              )}
            </div>
          </>
        )}
      </MentorAdminLayout>
    </>
  )
}

export default function MentorPastRequestsPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <PastRequestsContent />
    </MentorAuthProvider>
  )
}
