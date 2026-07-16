import { useEffect } from 'react'
import Link from 'next/link'
import Head from 'next/head'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { Footer, HtmlContent, MetaHeader, NavHeader } from '@/components'
import MentorPortrait from '@/components/ui/MentorPortrait'
import PriceBadge, { classifyPrice } from '@/components/ui/PriceBadge'
import { getOneMentorBySlug } from '@/server/mentors-data'
import seo from '@/config/seo'
import analytics from '@/lib/analytics'
import pluralize from '@/lib/pluralize'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import type { MentorBase } from '@/types'

interface MentorPageProps {
  [key: string]: unknown
  mentor: MentorBase
}

const _getServerSideProps: GetServerSideProps<MentorPageProps> = async (context) => {
  const slugParam = context.params?.slug
  const slug = Array.isArray(slugParam) ? slugParam[0] : slugParam

  if (!slug) {
    logger.warn('Mentor slug missing in request', { ...getTraceContext() })
    return { notFound: true }
  }

  const mentor = await getOneMentorBySlug(slug)

  if (!mentor) {
    logger.warn('Mentor not found', { slug, ...getTraceContext() })
    return {
      notFound: true,
    }
  }

  logger.info('Mentor profile page rendered', {
    mentorId: mentor.id,
    mentorSlug: mentor.slug,
    mentorName: mentor.name,
    ...getTraceContext(),
  })

  return {
    props: {
      mentor,
    },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'mentor-detail')

/** Section eyebrow: Archivo 800 CAPS +3% (design 02 content column). */
function SectionLabel({ children }: { children: string }): JSX.Element {
  return <h2 className="mb-2.5 text-[15px] leading-none tracking-[0.03em] text-ink">{children}</h2>
}

/** Photo-card footer / mobile chip lead: sessions -> mentees -> experience. */
function mentorMetaLead(mentor: MentorBase): string {
  if (mentor.sessionsCount && mentor.sessionsCount > 0) {
    return `${mentor.sessionsCount} ${pluralize(mentor.sessionsCount, 'session')}`
  }
  if (mentor.menteeCount > 0) {
    return `${mentor.menteeCount} ${pluralize(mentor.menteeCount, 'mentee')}`
  }
  return `${mentor.experience}y exp`
}

/** Mint-dot availability meta ("AVAILABLE" per design 02). */
function AvailabilityMeta({ isVisible }: { isVisible: boolean }): JSX.Element {
  return (
    <span
      className={
        isVisible
          ? 'meta-mono flex items-center gap-1.5 text-mint-ink'
          : 'meta-mono flex items-center gap-1.5 text-ink-soft'
      }
    >
      <span
        className={
          isVisible
            ? 'h-[7px] w-[7px] rounded-full bg-brand-mint'
            : 'h-[7px] w-[7px] rounded-full bg-ink-faint'
        }
      />
      {isVisible ? 'Available' : 'Paused'}
    </span>
  )
}

/** Big price value for the sidebar card / mobile CTA bar. */
function PriceValue({ price, size }: { price: string; size: 'lg' | 'sm' }): JSX.Element {
  const { kind } = classifyPrice(price)

  if (kind === 'free' || kind === 'negotiable') {
    return <PriceBadge price={price} />
  }

  return (
    <span
      className={
        size === 'lg'
          ? price.length <= 8
            ? 'font-name text-2xl font-bold leading-tight text-brand-navy'
            : 'font-name text-lg font-bold leading-tight text-brand-navy'
          : 'font-name text-[19px] font-bold leading-tight text-brand-navy'
      }
    >
      {price}
    </span>
  )
}

export default function Mentor({
  mentor,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  const title = mentor.name + ' | ' + seo.title

  useEffect(() => {
    analytics.event(analytics.events.MENTOR_PROFILE_VIEWED, {
      mentor_id: mentor.mentorId,
      mentor_slug: mentor.slug,
      mentor_experience_years: mentor.experience,
      mentor_price_tier: mentor.price,
      mentee_count: mentor.menteeCount,
      is_visible: mentor.isVisible,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // Intentionally run once on mount - analytics tracking

  const contactHref = '/mentor/' + mentor.slug + '/contact'
  const metaLead = mentorMetaLead(mentor)

  return (
    <>
      <Head>
        <title>{title}</title>

        <MetaHeader
          customTitle={mentor.name}
          customDescription={mentor.job + ' @ ' + mentor.workplace}
          customImage={mentor.photo_url}
        />
      </Head>

      <NavHeader backLink={{ href: '/', label: 'Back to mentors' }} />

      {/* Mobile: full-bleed pastel portrait with meta chips (design 02 · 390). */}
      <div className="relative md:hidden">
        <MentorPortrait
          mentor={mentor}
          quality="large"
          sizes="100vw"
          priority
          className="h-[270px]"
        />
        <span className="meta-mono absolute left-3.5 top-3.5 rounded-md bg-white/90 px-2 py-1 text-[10px] tracking-[0.05em] text-brand-navy">
          {metaLead}
        </span>
        <span className="absolute right-3.5 top-3.5 rounded-md bg-white/90 px-2 py-1 text-[10px]">
          <AvailabilityMeta isVisible={mentor.isVisible} />
        </span>
      </div>

      <div className="mx-auto flex max-w-[1200px] animate-rise-in items-start gap-11 px-5 pb-14 pt-5 md:px-8 md:pt-11 lg:px-16">
        {/* Sidebar (desktop): sticky photo + price + details cards. */}
        <aside className="sticky top-6 hidden w-[320px] flex-none flex-col gap-[18px] md:flex">
          <div className="overflow-hidden rounded-panel border border-line">
            <MentorPortrait
              mentor={mentor}
              quality="large"
              sizes="320px"
              priority
              className="h-[310px]"
            />
            <div className="flex items-center justify-between border-t border-line bg-white px-[18px] py-3.5">
              <span className="meta-mono text-ink-mute">{metaLead}</span>
              <AvailabilityMeta isVisible={mentor.isVisible} />
            </div>
          </div>

          <div className="flex flex-col gap-3 rounded-panel border border-line bg-white p-5">
            <div className="flex items-baseline justify-between">
              <span className="font-display text-sm font-extrabold uppercase tracking-[0.02em] text-ink">
                Price
              </span>
              <PriceValue price={mentor.price} size="lg" />
            </div>

            {mentor.isVisible ? (
              <Link href={contactHref} className="button w-full py-[15px] text-[15px]">
                Send request
              </Link>
            ) : (
              <div className="text-center text-sm text-ink-soft">
                This mentor is temporarily not accepting new requests.
              </div>
            )}

            <span className="text-center text-xs leading-normal text-ink-soft">
              Free to contact · no commission · the mentor replies by email
            </span>
          </div>

          <div className="flex flex-col gap-2.5 rounded-panel border border-line px-5 py-[18px]">
            <div className="flex justify-between">
              <span className="text-[13px] text-ink-soft">Experience</span>
              <span className="text-[13px] font-semibold text-ink">{mentor.experience} years</span>
            </div>
            {mentor.menteeCount > 0 && (
              <div className="flex justify-between">
                <span className="text-[13px] text-ink-soft">Helped</span>
                <span className="text-[13px] font-semibold text-ink">
                  {mentor.menteeCount} {pluralize(mentor.menteeCount, 'mentee')}
                </span>
              </div>
            )}
          </div>
        </aside>

        {/* Content column. */}
        <div className="flex min-w-0 flex-1 flex-col gap-[26px]">
          <div>
            <h1 className="font-name text-[28px] font-bold normal-case leading-[1.05] tracking-[-0.02em] text-ink md:text-[40px]">
              {mentor.name}
            </h1>
            <div className="mt-1.5 text-sm text-ink-soft md:text-[17px]">
              {mentor.job} · {mentor.workplace}
            </div>
            {/* Mobile-only mono meta line (design 02 · 390). */}
            <div className="meta-mono mt-2 text-ink-mute md:hidden">
              {mentor.experience}y exp
              {mentor.menteeCount > 0 &&
                ` · ${mentor.menteeCount} ${pluralize(mentor.menteeCount, 'mentee')}`}
            </div>
          </div>

          {mentor.tags.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {mentor.tags.map((tag) => (
                <span
                  key={tag}
                  className="rounded-full border border-line bg-surface px-[15px] py-2 text-[13px] font-semibold text-brand-navy"
                >
                  {tag}
                </span>
              ))}
            </div>
          )}

          {mentor.about && (
            <section>
              <SectionLabel>About</SectionLabel>
              <HtmlContent
                content={mentor.about}
                className="prose max-w-[640px] text-[15px] leading-[1.65] text-ink"
              />
            </section>
          )}

          {mentor.description && (
            <section>
              <SectionLabel>How I can help</SectionLabel>
              <HtmlContent
                content={mentor.description}
                className="prose max-w-[640px] text-[15px] leading-[1.65] text-ink"
              />
            </section>
          )}

          {mentor.competencies && (
            <section>
              <SectionLabel>Skills</SectionLabel>
              <p className="my-0 max-w-[640px] text-[15px] leading-[1.65] text-ink-soft">
                {mentor.competencies}
              </p>
            </section>
          )}

          {!mentor.isVisible && (
            <div className="text-sm text-ink-soft md:hidden">
              This mentor is temporarily not accepting new requests.
            </div>
          )}
        </div>
      </div>

      {/* Mobile: sticky bottom CTA bar (design 02 · 390). */}
      {mentor.isVisible && (
        <div className="sticky bottom-0 z-10 flex items-center gap-2.5 border-t border-line bg-white/[0.94] px-5 py-3 backdrop-blur-md md:hidden">
          <div className="flex-none">
            <div className="text-[11px] text-ink-soft">Per session</div>
            <PriceValue price={mentor.price} size="sm" />
          </div>
          <Link href={contactHref} className="button flex-1 py-[15px] text-[15px]">
            Send request
          </Link>
        </div>
      )}

      <Footer />
    </>
  )
}
