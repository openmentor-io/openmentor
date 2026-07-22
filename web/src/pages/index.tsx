import Head from 'next/head'
import Link from 'next/link'
import { useEffect, useMemo, useState } from 'react'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import type { ReactNode } from 'react'
import {
  MentorsFilters,
  MentorsList,
  MentorsSearch,
  MentorsSort,
  MetaHeader,
  NavHeader,
  Footer,
  sortMentors,
  useMentors,
} from '@/components'
import type { MentorsSortOption } from '@/components'
import { getAllMentors } from '@/server/mentors-data'
import analytics from '@/lib/analytics'
import pluralize from '@/lib/pluralize'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import type { MentorListItem } from '@/types'

interface HomePageProps {
  [key: string]: unknown
  pageMentors: MentorListItem[]
}

const _getServerSideProps: GetServerSideProps<HomePageProps> = async (context) => {
  const pageMentors = await getAllMentors({ onlyVisible: true, drop_long_fields: true })

  logger.info('Index page rendered', {
    mentorCount: pageMentors.length,
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {
      pageMentors,
    },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'index')

/**
 * Rounded community-size phrasing for the hero copy: exact under 10,
 * otherwise rounded down ("140+", "5,200+") — never overstates.
 */
function roundedCountLabel(count: number): string {
  if (count < 10) return `${count}`
  const step = count >= 100 ? 100 : 10
  return `${(Math.floor(count / step) * step).toLocaleString('en-US')}+`
}

interface HowItWorksCardProps {
  glyph: ReactNode
  title: string
  /** Number prefix ("1 · ") is shown on desktop only, per the design. */
  step: number
  copy: string
  copyShort: string
}

/**
 * "How it works" card: geometric brand glyph + Schibsted title. Desktop:
 * stacked white card (radius 16). Mobile: compact horizontal row.
 * The glyph box is 40px on desktop so the three glyph shapes baseline-align.
 */
function HowItWorksCard({ glyph, title, step, copy, copyShort }: HowItWorksCardProps) {
  return (
    <div className="flex items-center gap-3.5 rounded-card border border-line bg-white p-4 sm:block sm:rounded-panel sm:p-[26px]">
      <div className="flex w-8 flex-none items-center justify-center sm:mb-4 sm:h-10 sm:w-10 sm:justify-start">
        {glyph}
      </div>

      <div>
        <div className="font-name text-sm font-bold text-ink sm:text-lg">
          <span className="hidden sm:inline">{step} · </span>
          {title}
        </div>
        <p className="my-0 mt-0.5 text-xs leading-[1.4] text-ink-soft sm:hidden">{copyShort}</p>
        <p className="my-0 mt-2 hidden text-sm leading-[1.55] text-ink-soft sm:block">{copy}</p>
      </div>
    </div>
  )
}

export default function Home({
  pageMentors,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  const [
    mentors,
    searchInput,
    hasMoreMentors,
    setSearchInput,
    showMoreMentors,
    appliedFilters,
    filteredCount,
  ] = useMentors(pageMentors)

  const [sort, setSort] = useState<MentorsSortOption>('relevance')
  const sortedMentors = useMemo(() => sortMentors(mentors, sort), [mentors, sort])

  useEffect(() => {
    analytics.event(analytics.events.HOME_PAGE_VIEWED)
  }, [])

  const handleShowMoreMentors = (): void => {
    analytics.event(analytics.events.MENTORS_LIST_LOAD_MORE_CLICKED, {
      visible_count: mentors.length,
      total_count: pageMentors.length,
      active_filters_count: appliedFilters.count(),
    })
    showMoreMentors()
  }

  const scrollToList = (): void => {
    document.getElementById('list')?.scrollIntoView({ behavior: 'smooth' })
  }

  const countLabel = roundedCountLabel(pageMentors.length)

  // Results meta: the narrowed tag wins over the category label.
  const activeFilterLabel = appliedFilters.tags.values.length
    ? appliedFilters.tags.values.join(' · ')
    : appliedFilters.category.values ?? null

  return (
    <>
      <Head>
        <title>{seo.title}</title>
        <MetaHeader />
      </Head>

      <NavHeader />

      {/* ── Hero ─────────────────────────────────────────────────────── */}
      <section data-section="header" className="px-5 pt-7 sm:px-8 sm:pt-12 lg:px-16 lg:pt-16">
        <h1 className="text-[38px] leading-none sm:text-5xl md:text-6xl lg:text-7xl lg:leading-[0.98]">
          Your mentorship
          <br className="hidden md:block" /> journey{' '}
          <span className="text-brand-cobalt">starts</span>{' '}
          <span className="relative z-0">
            here
            <span
              aria-hidden="true"
              className="absolute inset-x-0 bottom-0.5 -z-10 h-2 bg-brand-mint opacity-[0.55] lg:bottom-1 lg:h-3"
            />
          </span>
        </h1>

        <p className="my-0 mt-3 max-w-[520px] text-sm leading-[1.5] text-ink-soft sm:mt-5 sm:text-[17px] sm:leading-[1.55]">
          An open community of {countLabel} tech mentors ready to share their experience one on one.
          Free to browse, zero commission — many mentor for free.
        </p>

        <div className="mt-[18px] flex flex-col gap-2.5 sm:mt-[30px] sm:flex-row sm:items-center sm:gap-3">
          <div className="w-full sm:max-w-[460px]">
            <MentorsSearch value={searchInput} onChange={setSearchInput} />
          </div>
          <button
            type="button"
            className="button whitespace-nowrap px-[26px] py-[15px] text-[15px] sm:py-4"
            onClick={scrollToList}
          >
            Find a mentor
          </button>
        </div>

        {/* Trust strip: the mission in one glance, visible without scrolling. */}
        <p className="meta-mono mb-0 mt-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-ink-mute sm:mt-5">
          <span>No account needed</span>
          <span aria-hidden="true">·</span>
          <span>0% commission</span>
          <span aria-hidden="true">·</span>
          <span>Many mentors free</span>
          <span aria-hidden="true">·</span>
          <span>Donation-funded</span>
        </p>
      </section>

      {/* ── Catalog: filters + results meta + grid ───────────────────── */}
      <section
        id="list"
        data-section="list"
        className="scroll-mt-4 px-5 pt-[18px] sm:px-8 sm:pt-[34px] lg:px-16"
      >
        <h2 className="sr-only">Our mentors</h2>

        <MentorsFilters appliedFilters={appliedFilters} mentors={pageMentors} />

        <div className="flex items-baseline justify-between gap-4 pb-3 pt-[18px] sm:pb-3.5 sm:pt-[26px]">
          <span className="font-display text-sm font-extrabold uppercase tracking-[0.02em] text-ink sm:text-[17px]">
            {filteredCount.toLocaleString('en-US')} {pluralize(filteredCount, 'mentor')}
            {activeFilterLabel && <span className="text-brand-cobalt"> · {activeFilterLabel}</span>}
          </span>

          <MentorsSort value={sort} onChange={setSort} />
        </div>

        <div className="pb-6 sm:pb-11">
          <MentorsList
            mentors={sortedMentors}
            hasMore={hasMoreMentors}
            onClickMore={handleShowMoreMentors}
          />
        </div>
      </section>

      {/* ── How it works + the mission behind it ─────────────────────── */}
      <section
        data-section="howitworks"
        className="border-t border-line bg-surface px-5 py-7 sm:px-8 sm:py-14 lg:px-16"
      >
        <h2 className="mb-[18px] text-2xl leading-none sm:mb-[34px] sm:text-[34px]">
          How it works
        </h2>

        <div className="grid gap-2.5 sm:grid-cols-3 sm:gap-7">
          <HowItWorksCard
            step={1}
            title="Browse freely"
            glyph={
              <div className="h-[26px] w-[26px] rounded-full border-4 border-brand-navy sm:h-10 sm:w-10 sm:border-[5px]" />
            }
            copy={`No account needed. Search ${countLabel} mentors by role, skill, or company and read their full profiles.`}
            copyShort="No account needed."
          />

          <HowItWorksCard
            step={2}
            title="Send a request"
            glyph={
              <div className="h-1.5 w-6 origin-left -rotate-[35deg] rounded-full bg-brand-cobalt sm:h-2 sm:w-10" />
            }
            copy="Tell the mentor what you want to work on. They reply by email — usually within a couple of days."
            copyShort="Mentors reply by email."
          />

          <HowItWorksCard
            step={3}
            title="Meet & grow"
            glyph={<div className="h-3 w-3 rounded-full bg-brand-mint sm:h-4 sm:w-4" />}
            copy="Agree on format and price together. Many mentors are free; if you pay, you pay the mentor directly — we never touch the money."
            copyShort="Zero commission, ever."
          />
        </div>

        {/* The mission behind the mechanics, in one breath (full story: /about). */}
        <p className="my-0 mt-5 max-w-[760px] text-sm leading-[1.6] text-ink-soft sm:mt-8 sm:text-[15px]">
          <span className="font-semibold text-ink">Built on community.</span> No ads, no commission,
          no premium tier — donations cover the servers, and mentors who give their time for free
          are the heart of this place. Every mentor is a working practitioner, reviewed by hand
          before they appear: no gurus, no 10x promises. We make the connection, then get out of the
          way.{' '}
          <Link href="/about" className="whitespace-nowrap font-semibold text-brand-cobalt">
            Read the full story →
          </Link>
        </p>
      </section>

      {/* ── Become a mentor: the page said nothing to practitioners ───── */}
      <section
        data-section="bementor"
        className="border-t border-line bg-white px-5 py-8 text-center sm:px-8 sm:py-14 lg:px-16"
      >
        <h2 className="my-0 text-2xl leading-none sm:text-[34px]">
          Been there? <span className="text-brand-cobalt">Pass it on</span>
        </h2>
        <p className="mx-auto mb-0 mt-2.5 max-w-[520px] text-sm leading-[1.55] text-ink-soft sm:mt-4 sm:text-[15px] sm:leading-[1.6]">
          Share what you know one on one — you choose the format and the price, and mentoring for
          free is warmly encouraged. Reviews are done by hand, usually within a week.
        </p>
        <div className="mt-5 flex flex-col items-center justify-center gap-2.5 sm:mt-7 sm:flex-row">
          <Link href="/bementor" className="button px-[30px] py-[15px] text-[15px]">
            Become a mentor
          </Link>
          <Link href="/faq#mentors" className="button-ghost px-[26px] py-[15px] text-[15px]">
            Read the mentor FAQ
          </Link>
        </div>
      </section>

      {/* ── Support us ───────────────────────────────────────────────── */}
      <section
        data-section="support"
        className="border-t border-line bg-white px-5 py-6 sm:px-8 sm:py-14 lg:px-16"
      >
        <div className="relative mx-auto flex max-w-[1100px] flex-col items-center gap-3 overflow-hidden rounded-panel bg-pastel-sky-grad px-5 py-6 text-center lg:flex-row lg:items-center lg:gap-11 lg:rounded-[20px] lg:px-[52px] lg:py-11 lg:text-left">
          {/* decorative oversized ring, overflowing top-right */}
          <div
            aria-hidden="true"
            className="absolute -right-[70px] -top-[70px] hidden h-[260px] w-[260px] rounded-full border-[34px] border-brand-navy/[0.08] lg:block"
          />

          {/* CSS logomark composition (ring + bar + dot) */}
          <div aria-hidden="true" className="relative h-14 w-14 flex-none lg:h-24 lg:w-24">
            <div className="m-1.5 h-11 w-11 rounded-full border-[6px] border-brand-navy lg:m-[9px] lg:h-[78px] lg:w-[78px] lg:border-[10px]" />
            <div className="absolute bottom-3.5 hidden h-2.5 w-11 -rotate-[35deg] rounded-full bg-brand-cobalt lg:-left-4 lg:block" />
            <div className="absolute right-0 top-0.5 h-[13px] w-[13px] rounded-full bg-brand-mint lg:right-1 lg:top-1 lg:h-5 lg:w-5" />
          </div>

          <div className="relative flex-1">
            <h2 className="my-0 text-xl leading-[1.05] sm:text-[26px] lg:text-[32px] lg:leading-[1.02]">
              Free because <span className="text-brand-cobalt">you</span> keep it free
            </h2>
            <p className="mx-auto my-0 mt-2 max-w-[520px] text-[13px] leading-[1.5] text-ink-mute lg:mx-0 lg:mt-2.5 lg:hidden">
              Donations are our only income. No ads, no commission.
            </p>
            <p className="my-0 mt-2.5 hidden max-w-[520px] text-[15px] leading-[1.55] text-ink-mute lg:block">
              Donations are our only income — no ads, no commission, no premium tier. A few dollars
              covers servers and email for everyone.
            </p>
          </div>

          <div className="relative flex w-full flex-none flex-col items-center gap-2.5 lg:w-auto">
            <Link
              href="/donate"
              className="button w-full px-[30px] py-[15px] text-[15px] lg:w-auto lg:py-4"
            >
              Support us ↗
            </Link>
            <span className="meta-mono hidden text-ink-mute lg:block">VIA KO-FI · FROM $3</span>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
