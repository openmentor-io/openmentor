/**
 * Admin — requests a mentor received.
 *
 * Reached from the "Requests (N)" button on the mentor edit page. Lists every
 * request across all statuses with a status filter and a text search; each row
 * opens the request page, where the status can be edited.
 */

import { useEffect, useMemo, useState } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import classNames from 'classnames'
import type {
  AdminMentorDetails,
  AdminRequestStatusFilter,
  MentorClientRequest,
  RequestStatus,
} from '@/types'
import { ALL_REQUEST_STATUSES, STATUS_LABELS } from '@/types'
import { AdminAuthProvider, AdminLayout, useAdminAuth } from '@/components/admin-moderation'
import { StatusBadge, formatDateTime, formatRelativeTime } from '@/components/mentor-admin'
import { getModerationMentorById, getModerationMentorRequests } from '@/lib/admin-moderation-api'

function matchesQuery(request: MentorClientRequest, query: string): boolean {
  if (!query) return true
  const haystack = [request.name, request.email, request.contact, request.details, request.id]
  return haystack.some((value) => (value || '').toLowerCase().includes(query))
}

function AdminMentorRequestsContent(): JSX.Element {
  const router = useRouter()
  const { id } = router.query
  const mentorId = Array.isArray(id) ? id[0] : id
  const { isAuthenticated, isLoading: authLoading, session } = useAdminAuth()

  const [mentor, setMentor] = useState<AdminMentorDetails | null>(null)
  const [requests, setRequests] = useState<MentorClientRequest[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState<AdminRequestStatusFilter>('all')
  const [searchQuery, setSearchQuery] = useState('')

  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/admin/login')
      return
    }
    // Requests are admin-only; the backend rejects moderators outright.
    if (!authLoading && session && session.role !== 'admin') {
      router.replace('/admin/mentors/pending')
    }
  }, [authLoading, isAuthenticated, router, session])

  useEffect(() => {
    if (!isAuthenticated || session?.role !== 'admin' || !mentorId) return

    let mounted = true
    const load = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const [mentorData, requestsData] = await Promise.all([
          getModerationMentorById(mentorId),
          getModerationMentorRequests(mentorId),
        ])
        if (!mounted) return
        if (!mentorData) {
          setError('Mentor not found')
          return
        }
        setMentor(mentorData)
        setRequests(requestsData)
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : 'Failed to load requests')
        }
      } finally {
        if (mounted) setIsLoading(false)
      }
    }

    load()
    return () => {
      mounted = false
    }
  }, [isAuthenticated, session, mentorId])

  const filterOptions = useMemo(() => {
    const counts = new Map<RequestStatus, number>()
    for (const request of requests) {
      counts.set(request.status, (counts.get(request.status) || 0) + 1)
    }
    return [
      { value: 'all' as AdminRequestStatusFilter, label: 'All', count: requests.length },
      ...ALL_REQUEST_STATUSES.map((status) => ({
        value: status as AdminRequestStatusFilter,
        label: STATUS_LABELS[status],
        count: counts.get(status) || 0,
      })),
    ]
  }, [requests])

  const visibleRequests = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()
    return requests.filter(
      (request) =>
        (statusFilter === 'all' || request.status === statusFilter) && matchesQuery(request, query)
    )
  }, [requests, statusFilter, searchQuery])

  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <AdminLayout title="Mentor requests">
      <Head>
        <title>Mentor requests — openmentor.io</title>
      </Head>

      {mentorId && (
        <div className="mb-4">
          <Link
            href={`/admin/mentors/${encodeURIComponent(mentorId)}`}
            className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
          >
            ← Back to mentor
          </Link>
        </div>
      )}

      {mentor && (
        <div className="mb-4">
          <h2 className="my-0 font-name text-lg font-bold tracking-[-0.015em] text-ink">
            {mentor.name}
          </h2>
          <p className="my-0 text-sm text-ink-soft">{mentor.email}</p>
        </div>
      )}

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

      {!isLoading && !error && (
        <>
          <div
            className="no-scrollbar mb-4 flex gap-1.5 overflow-x-auto"
            role="group"
            aria-label="Filter by status"
          >
            {filterOptions.map((option) => {
              const isActive = option.value === statusFilter
              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => setStatusFilter(option.value)}
                  aria-pressed={isActive}
                  className={classNames(
                    'flex-none rounded-full text-xs font-semibold transition-colors duration-120',
                    isActive
                      ? 'bg-brand-navy px-3.5 py-2 text-white'
                      : 'border-[1.5px] border-line bg-white px-[13px] py-[6.5px] text-brand-navy hover:border-brand-cobalt/45'
                  )}
                >
                  {option.label} <span className="font-mono font-bold">· {option.count}</span>
                </button>
              )
            })}
          </div>

          <div className="mb-4">
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search by name, email, contact, message"
              className="field"
            />
          </div>

          <div className="meta-mono mb-4 text-ink-mute">
            Showing {visibleRequests.length} of {requests.length}
          </div>

          {visibleRequests.length === 0 && (
            <div className="rounded-card border border-line bg-white p-6 text-sm text-ink-soft">
              {requests.length === 0
                ? 'This mentor has not received any requests yet.'
                : 'No requests match the current filter.'}
            </div>
          )}

          {visibleRequests.length > 0 && mentorId && (
            <div className="space-y-2.5">
              {visibleRequests.map((request) => (
                <Link
                  key={request.id}
                  href={`/admin/mentors/${encodeURIComponent(mentorId)}/requests/${encodeURIComponent(request.id)}`}
                  className="block rounded-card border border-line bg-white p-4 transition-all duration-180 hover:shadow-card-hover"
                >
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="my-0 font-name text-base font-bold tracking-[-0.015em] text-ink">
                        {request.name}
                      </p>
                      <p className="my-0 text-sm text-ink-soft">{request.email}</p>
                      {request.level && <p className="my-0 text-sm text-ink-mute">{request.level}</p>}
                    </div>
                    <StatusBadge status={request.status} />
                  </div>
                  <p className="my-0 mt-3 line-clamp-2 text-sm text-ink">{request.details}</p>
                  <p className="meta-mono my-0 mt-2 text-ink-mute">
                    {formatDateTime(request.createdAt)} · {formatRelativeTime(request.createdAt)}
                  </p>
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </AdminLayout>
  )
}

export default function AdminMentorRequestsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <AdminMentorRequestsContent />
    </AdminAuthProvider>
  )
}
