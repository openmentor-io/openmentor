import Head from 'next/head'
import Link from 'next/link'
import { useEffect, useState } from 'react'
import { InlineWidget } from 'react-calendly'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { CalendlabWidget, ContactMentorForm, Footer, Koalendar, NavHeader } from '@/components'
import MentorPortrait from '@/components/ui/MentorPortrait'
import PriceBadge, { classifyPrice } from '@/components/ui/PriceBadge'
import seo from '@/config/seo'
import { getOneMentorBySlug } from '@/server/mentors-data'
import analytics from '@/lib/analytics'
import { safeHttpUrl } from '@/lib/safe-url'
import { captureException } from '@/lib/posthog'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import pluralize from '@/lib/pluralize'
import type { MentorBase } from '@/types'

// Rate limiting configuration
const RATE_LIMIT_CONFIG = {
  MAX_REQUESTS_PER_DAY: 5,
  STORAGE_KEY: 'requests_per_day',
}

type ReadyStatus = '' | 'loading' | 'success' | 'error' | 'limit'
type MentorContact = MentorBase & { calendarUrl?: string | null }

interface ContactFormData {
  email: string
  name: string
  intro: string
  experience?: string
  contact: string
  captchaToken: string
}

const _getServerSideProps: GetServerSideProps<{ mentor: MentorContact }> = async (context) => {
  const slugParam = context.params?.slug
  const slug = Array.isArray(slugParam) ? slugParam[0] : slugParam

  if (!slug) {
    logger.warn('Mentor slug missing on contact page', { ...getTraceContext() })
    return { notFound: true }
  }

  const mentor = await getOneMentorBySlug(slug)

  if (!mentor) {
    logger.warn('Mentor not found for contact page', { slug, ...getTraceContext() })
    return {
      notFound: true,
    }
  }

  logger.info('Mentor contact page rendered', {
    mentorId: mentor.id,
    mentorSlug: mentor.slug,
    calendarType: mentor.calendarType,
    ...getTraceContext(),
  })

  return {
    props: {
      mentor,
    },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'mentor-contact')

/** Small inline price for the recap card / mobile strip. */
function RecapPrice({ price }: { price: string }): JSX.Element {
  const { kind } = classifyPrice(price)
  if (kind === 'free' || kind === 'negotiable') {
    return <PriceBadge price={price} />
  }
  return <span className="font-name text-[15px] font-bold text-brand-navy">{price}</span>
}

/** Desktop mentor recap card (design 03, right column). */
function MentorRecapCard({ mentor }: { mentor: MentorContact }): JSX.Element {
  const metaLead =
    mentor.sessionsCount && mentor.sessionsCount > 0
      ? `${mentor.sessionsCount} ${pluralize(mentor.sessionsCount, 'session')}`
      : `${mentor.experience}y exp`

  return (
    <div className="hidden w-[300px] flex-none overflow-hidden rounded-panel border border-line lg:block">
      <MentorPortrait
        mentor={mentor}
        quality="large"
        sizes="300px"
        className="h-[180px]"
        heroBoxClassName="h-[89%] w-1/2 max-w-[150px]"
        frameBoxClassName="h-[80%] w-[42%] max-w-[130px]"
        initialsClassName="h-16 w-16 text-2xl"
      />
      <div className="border-t border-line px-[18px] py-4">
        <div className="font-name text-[17px] font-bold leading-tight text-ink">{mentor.name}</div>
        <div className="mt-0.5 text-[13px] text-ink-soft">
          {mentor.job} · {mentor.workplace}
        </div>
        <div className="mt-2.5 flex items-center justify-between gap-2">
          <span className="meta-mono text-ink-mute">{metaLead}</span>
          <RecapPrice price={mentor.price} />
        </div>
      </div>
    </div>
  )
}

/** Mobile mentor recap strip under the header (design 03 · 390). */
function MentorRecapStrip({ mentor }: { mentor: MentorContact }): JSX.Element {
  return (
    <div className="flex items-center gap-3 border-b border-line bg-surface px-5 py-4 lg:hidden">
      <MentorPortrait
        mentor={mentor}
        quality="small"
        sizes="52px"
        className="h-[52px] w-[52px] flex-none rounded-field"
        heroBoxClassName="h-[92%] w-[85%]"
        frameBoxClassName="h-[85%] w-[80%] rounded-t-[8px] border-2"
        initialsClassName="h-8 w-8 text-xs"
      />
      <div className="min-w-0">
        <div className="truncate font-name text-[15px] font-bold text-ink">{mentor.name}</div>
        <div className="truncate text-xs text-ink-soft">
          {mentor.job} · {mentor.workplace} · {mentor.price}
        </div>
      </div>
    </div>
  )
}

export default function OrderMentor({
  mentor,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  const [readyStatus, setReadyStatus] = useState<ReadyStatus>('')
  const [formData, setFormData] = useState<ContactFormData | undefined>()
  const [submissionRequestId, setSubmissionRequestId] = useState<string | undefined>()

  const today = new Date().toISOString().slice(0, 10)
  const title = 'Contact a mentor | ' + mentor.name + ' | ' + seo.title
  const firstName = mentor.name.split(' ')[0]

  // Helper function to get current request count from localStorage
  const getRequestsToday = (): number => {
    const storage = window.localStorage.getItem(RATE_LIMIT_CONFIG.STORAGE_KEY)
    if (storage !== null) {
      const nr_requests = JSON.parse(storage) as Record<string, number>
      return nr_requests[today] || 0
    }
    return 0
  }

  const hasRequestPerDayLeft = (): boolean => {
    const requestsToday = getRequestsToday()
    return requestsToday < RATE_LIMIT_CONFIG.MAX_REQUESTS_PER_DAY
  }

  const incrementRequestsPerDay = (): void => {
    const requestsToday = getRequestsToday()
    const nr_requests: Record<string, number> = {}
    nr_requests[today] = requestsToday + 1
    window.localStorage.setItem(RATE_LIMIT_CONFIG.STORAGE_KEY, JSON.stringify(nr_requests))
  }

  useEffect(() => {
    analytics.event(analytics.events.MENTOR_CONTACT_PAGE_VIEWED, {
      mentor_id: mentor.mentorId,
      mentor_slug: mentor.slug,
      mentor_experience_years: mentor.experience,
      mentor_price_tier: mentor.price,
      is_visible: mentor.isVisible,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // Intentionally run once on mount - analytics tracking

  useEffect(() => {
    if (!hasRequestPerDayLeft()) {
      setReadyStatus('limit')
      analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
        mentor_id: mentor.mentorId,
        mentor_slug: mentor.slug,
        outcome: 'rate_limited',
      })
      return
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // Intentionally run once on mount - check rate limit

  const onSubmit = (data: ContactFormData): void => {
    if (readyStatus === 'loading') {
      return
    }

    if (!hasRequestPerDayLeft()) {
      setReadyStatus('limit')
      analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
        mentor_id: mentor.mentorId,
        mentor_slug: mentor.slug,
        outcome: 'rate_limited',
      })
      return
    }

    setReadyStatus('loading')
    analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
      mentor_id: mentor.mentorId,
      mentor_slug: mentor.slug,
      experience: data.experience,
      has_contact: Boolean(data.contact),
      outcome: 'submitted',
    })

    setFormData({ ...data })

    // SECURITY: Call Next.js API route (proxy), which calls Go API on localhost
    // This keeps Go API private (localhost only), not exposed to public internet
    fetch('/api/contact-mentor', {
      method: 'POST',
      body: JSON.stringify({
        ...data,
        mentorId: mentor.mentorId,
      }),
      headers: {
        'Content-Type': 'application/json',
      },
    })
      .then((res) => {
        if (!res.ok) {
          analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
            mentor_id: mentor.mentorId,
            mentor_slug: mentor.slug,
            outcome: 'error',
            status_code: res.status,
          })
          throw new Error(`HTTP error! status: ${res.status}`)
        }
        return res.json() as Promise<{
          success: boolean
          requestId?: string
          calendar_url?: string
        }>
      })
      .then((responseData) => {
        if (responseData.success) {
          setSubmissionRequestId(responseData.requestId)
          mentor.calendarUrl = responseData.calendar_url
          setReadyStatus('success')
          incrementRequestsPerDay()
          analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
            mentor_id: mentor.mentorId,
            mentor_slug: mentor.slug,
            request_id: responseData.requestId,
            calendar_url_available: Boolean(responseData.calendar_url),
            outcome: 'success',
          })
        } else {
          setReadyStatus('error')
          analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
            mentor_id: mentor.mentorId,
            mentor_slug: mentor.slug,
            outcome: 'error',
            error_type: 'api_error',
          })
        }
      })
      .catch((e) => {
        setReadyStatus('error')
        if (e instanceof Error) {
          captureException(e, { page: 'contact-mentor', mentorSlug: mentor.slug })
        }
        analytics.event(analytics.events.MENTEE_CONTACT_SUBMITTED, {
          mentor_id: mentor.mentorId,
          mentor_slug: mentor.slug,
          outcome: 'error',
          error_type: e instanceof Error ? e.name : 'network_error',
        })
        console.error('Contact mentor error:', e)
      })
  }

  const showForm = mentor.isVisible && readyStatus !== 'success' && readyStatus !== 'limit'

  return (
    <>
      <Head>
        <title>{title}</title>
      </Head>

      <NavHeader backLink={{ href: '/mentor/' + mentor.slug, label: 'Back to profile' }} />

      {showForm && <MentorRecapStrip mentor={mentor} />}

      <main className="mx-auto flex w-full max-w-[1100px] animate-rise-in items-start gap-14 px-5 pb-16 pt-9 md:px-8 md:pt-12 lg:px-16">
        {!mentor.isVisible && (
          <div className="flex-1 py-10 text-center text-ink-soft">
            This mentor is temporarily not accepting new requests.
          </div>
        )}

        {mentor.isVisible && readyStatus === 'success' && (
          <SuccessMessage mentor={mentor} formData={formData} requestId={submissionRequestId} />
        )}

        {mentor.isVisible && readyStatus === 'limit' && <LimitMessage />}

        {showForm && (
          <>
            <div className="min-w-0 max-w-[560px] flex-1">
              <h1 className="text-[24px] leading-[1.05] tracking-[-0.02em] md:text-[34px]">
                Request a session
              </h1>
              <p className="mb-7 mt-2.5 text-[15px] leading-relaxed text-ink-soft">
                {firstName} will get your message by email and reply directly to you. Be specific —
                mentors accept requests they can actually help with.
              </p>

              <ContactMentorForm
                isLoading={readyStatus === 'loading'}
                isError={readyStatus === 'error'}
                onSubmit={onSubmit}
                mentorFirstName={firstName}
              />
            </div>

            <MentorRecapCard mentor={mentor} />
          </>
        )}
      </main>

      <Footer />
    </>
  )
}

interface SuccessMessageProps {
  mentor: MentorContact
  formData?: ContactFormData
  requestId?: string
}

/** Success state (design 03): open ring + mint node, CAPS header, CTAs. */
function SuccessMessage({ mentor, formData, requestId }: SuccessMessageProps): JSX.Element {
  const firstName = mentor.name.split(' ')[0]

  return (
    <div className="flex flex-1 animate-rise-in flex-col items-center text-center">
      {/* Brand mark: open navy ring, mint node "arrives" top-right. */}
      <div className="relative h-[88px] w-[88px]" aria-hidden="true">
        <div className="m-2 h-[72px] w-[72px] rounded-full border-[9px] border-brand-navy" />
        <div className="absolute right-0.5 top-1.5 flex h-[22px] w-[22px] items-center justify-center rounded-full bg-brand-mint">
          <svg width="11" height="9" viewBox="0 0 11 9" fill="none">
            <path
              d="M1 4.5L4 7.5L10 1"
              stroke="#fff"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
      </div>

      <h2 className="mt-[22px] text-[28px] tracking-[-0.02em] text-ink">Request sent</h2>

      <p className="mb-0 mt-3 max-w-[380px] text-[15px] leading-relaxed text-ink-soft">
        {firstName} got your message and will reply directly to{' '}
        {formData?.email ? (
          <b className="font-semibold text-ink">{formData.email}</b>
        ) : (
          'your email'
        )}
        .
      </p>

      {requestId && <p className="meta-mono mb-0 mt-3 text-ink-mute">Request ID: {requestId}</p>}

      <div className="mt-6 flex flex-wrap justify-center gap-2.5">
        <Link href="/" className="button">
          Browse more mentors
        </Link>
        <Link href={'/mentor/' + mentor.slug} className="button-secondary">
          Back to profile
        </Link>
      </div>

      {mentor.calendarType !== 'none' && (
        <div className="mt-10 w-full max-w-screen-md">
          <p className="text-[15px] leading-relaxed text-ink-soft">
            You can also pick a convenient time for the session right now using the form below.
          </p>

          {mentor.calendarType === 'calendly' ? (
            <InlineWidget
              url={mentor.calendarUrl ?? ''}
              prefill={{
                name: formData?.name,
                email: formData?.email,
                customAnswers: {
                  a1: formData?.intro,
                },
              }}
            />
          ) : mentor.calendarType === 'koalendar' ? (
            <Koalendar url={mentor.calendarUrl ?? ''} />
          ) : mentor.calendarType === 'calendlab' ? (
            <CalendlabWidget url={mentor.calendarUrl ?? ''} />
          ) : safeHttpUrl(mentor.calendarUrl) ? (
            // SECURITY (M9): only render the link for a validated http(s) URL.
            <a
              className="button"
              href={safeHttpUrl(mentor.calendarUrl) as string}
              target="_blank"
              rel="noreferrer"
            >
              Book a session
            </a>
          ) : null}
        </div>
      )}
    </div>
  )
}

function LimitMessage(): JSX.Element {
  return (
    <div className="flex flex-1 animate-rise-in flex-col items-center py-10 text-center">
      <div
        className="flex h-[72px] w-[72px] items-center justify-center rounded-full bg-danger/10 text-danger"
        aria-hidden="true"
      >
        <svg width="28" height="28" viewBox="0 0 12 12" fill="none">
          <path d="M6 2v5M6 9.5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
      </div>
      <p className="mt-6 max-w-[420px] text-lg text-ink">
        You&apos;ve reached the daily request limit. Come back tomorrow to contact another mentor.
      </p>
    </div>
  )
}
