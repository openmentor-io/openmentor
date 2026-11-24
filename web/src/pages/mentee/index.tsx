/**
 * Mentee Dashboard (D95)
 *
 * Read-only list of every request sent from the signed-in email, grouped as
 * pending / active / completed / closed. The only action is leaving a review on
 * a completed request (and reading one already left).
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { Footer, NavHeader, NoIndexHead, AuthGateScreen } from '@/components'
import { MenteeAuthProvider, MenteeRequestCard, useMenteeAuth } from '@/components/mentee'
import { RequestListSkeleton } from '@/components/mentor-admin'
import { getMenteeRequests } from '@/lib/mentee-api'
import analytics from '@/lib/analytics'
import type { MenteeRequest, MenteeRequestGroup, MenteeReview } from '@/types'

const GROUPS: { group: MenteeRequestGroup; title: string; hint: string }[] = [
  {
    group: 'pending',
    title: 'Waiting for the mentor',
    hint: 'The mentor has your request and will reply by email.',
  },
  { group: 'active', title: 'In progress', hint: 'You and the mentor are in touch.' },
  { group: 'done', title: 'Completed', hint: 'Tell the mentor how it went.' },
  { group: 'closed', title: 'Closed', hint: 'Declined or no longer available.' },
]

function DashboardContent(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading, logout } = useMenteeAuth()
  const [requests, setRequests] = useState<MenteeRequest[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const viewTracked = useRef(false)

  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/mentee/login')
    }
  }, [authLoading, isAuthenticated, router])

  useEffect(() => {
    if (!isAuthenticated) return
    let mounted = true
    setIsLoading(true)
    setError(null)
    getMenteeRequests()
      .then((data) => {
        if (!mounted) return
        setRequests(data.requests)
        if (!viewTracked.current) {
          viewTracked.current = true
          analytics.event(analytics.events.MENTEE_DASHBOARD_VIEWED, {
            total: data.requests.length,
            reviewable: data.requests.filter((r) => r.canReview).length,
          })
        }
      })
      .catch((err: unknown) => {
        if (mounted) setError(err instanceof Error ? err.message : 'Failed to load requests')
      })
      .finally(() => mounted && setIsLoading(false))
    return () => {
      mounted = false
    }
  }, [isAuthenticated])

  const grouped = useMemo(() => {
    const map: Record<MenteeRequestGroup, MenteeRequest[]> = {
      pending: [],
      active: [],
      done: [],
      closed: [],
    }
    for (const request of requests) {
      ;(map[request.group] ?? map.closed).push(request)
    }
    return map
  }, [requests])

  const handleReviewed = (requestId: string, review: MenteeReview): void => {
    setRequests((current) =>
      current.map((r) => (r.id === requestId ? { ...r, review, canReview: false } : r)),
    )
  }

  const handleLogout = async (): Promise<void> => {
    await logout()
    router.push('/mentee/login')
  }

  if (authLoading || !isAuthenticated) {
    return <AuthGateScreen title="My requests" />
  }

  return (
    <>
      <NoIndexHead title="My requests" />
      <NavHeader />

      <main className="mx-auto w-full max-w-[820px] px-5 pb-16 pt-8 sm:pb-24 sm:pt-12">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · Mentee</p>
            <h1 className="my-0 mt-2 text-3xl sm:text-[40px]">My requests</h1>
          </div>
          <button
            type="button"
            onClick={handleLogout}
            className="text-sm font-semibold text-ink-soft transition-colors duration-120 hover:text-ink"
          >
            Log out
          </button>
        </div>

        {isLoading && (
          <div className="mt-8">
            <RequestListSkeleton />
          </div>
        )}

        {error && !isLoading && (
          <div className="mt-8 rounded-card border border-danger/40 bg-white p-5">
            <p className="my-0 text-sm font-medium text-danger">{error}</p>
            <button
              onClick={() => window.location.reload()}
              className="mt-2 text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              Try again
            </button>
          </div>
        )}

        {!isLoading && !error && requests.length === 0 && (
          <div className="mt-8 rounded-card border border-line bg-white p-8 text-center">
            <h2 className="my-0 text-xl">No requests yet</h2>
            <p className="my-0 mt-2 text-sm text-ink-soft">
              Requests you send to mentors from this email will show up here.
            </p>
            <Link href="/" className="button mt-5 inline-flex">
              Find a mentor
            </Link>
          </div>
        )}

        {!isLoading &&
          !error &&
          GROUPS.filter(({ group }) => grouped[group].length > 0).map(({ group, title, hint }) => (
            <section key={group} className="mt-10" aria-labelledby={`group-${group}`}>
              <div className="flex items-baseline gap-3">
                <h2 id={`group-${group}`} className="my-0 text-lg sm:text-xl">
                  {title}
                </h2>
                <span className="meta-mono text-ink-mute">{grouped[group].length}</span>
              </div>
              <p className="my-0 mt-1 text-sm text-ink-soft">{hint}</p>
              <div className="mt-4 flex flex-col gap-3">
                {grouped[group].map((request) => (
                  <MenteeRequestCard
                    key={request.id}
                    request={request}
                    onReviewed={handleReviewed}
                  />
                ))}
              </div>
            </section>
          ))}
      </main>

      <Footer />
    </>
  )
}

export default function MenteeDashboardPage(): JSX.Element {
  return (
    <MenteeAuthProvider>
      <DashboardContent />
    </MenteeAuthProvider>
  )
}
