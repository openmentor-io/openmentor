import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import { getAllMentors } from '@/server/mentors-data'
import { pageTitle } from '@/config/seo'
import constants from '@/config/constants'
import { jsonLdScriptProps } from '@/lib/json-ld'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import type { MentorListItem } from '@/types'

/**
 * Only the fields this page renders. Everything returned to props is
 * serialized into __NEXT_DATA__ on every request, and `drop_long_fields`
 * clears just `about`/`description` — a full MentorListItem still carries
 * `competencies` (accepted up to 5,000 chars), tags, counts and photo
 * metadata, none of which a directory row uses. At ~830 bytes per mentor
 * unprojected vs ~110 projected, that is the difference between a few
 * hundred KB and a few tens of KB once the catalog reaches its target size.
 */
type DirectoryMentor = Pick<MentorListItem, 'id' | 'slug' | 'name' | 'job' | 'workplace'>

interface MentorsPageProps {
  [key: string]: unknown
  mentors: DirectoryMentor[]
}

// Every VISIBLE mentor, not just the first page-load batch (see useMentors
// DEFAULT_PAGE_SIZE) — this page exists so mentors past that cutoff have a
// crawlable inbound link somewhere other than the sitemap.
const _getServerSideProps: GetServerSideProps<MentorsPageProps> = async (context) => {
  const allMentors = await getAllMentors({ onlyVisible: true, drop_long_fields: true })
  const mentors: DirectoryMentor[] = allMentors
    .map(({ id, slug, name, job, workplace }) => ({ id, slug, name, job, workplace }))
    .sort((a, b) => a.name.localeCompare(b.name))

  logger.info('Mentors directory page rendered', {
    mentorCount: mentors.length,
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {
      mentors,
    },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'mentors')

export default function MentorsDirectory({
  mentors,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  // Kept to the three properties schema.org needs for a ListItem — this list
  // runs to hundreds of entries, so anything more would bloat the HTML.
  const mentorsJsonLd: Record<string, unknown> = {
    '@context': 'https://schema.org',
    '@type': 'CollectionPage',
    name: 'All mentors',
    url: `${constants.BASE_URL}mentors`,
    mainEntity: {
      '@type': 'ItemList',
      numberOfItems: mentors.length,
      itemListElement: mentors.map((mentor, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        name: mentor.name,
        url: `${constants.BASE_URL}mentor/${mentor.slug}`,
      })),
    },
  }

  // Directory sits between Home and a mentor profile — mentor pages already
  // emit their own BreadcrumbList (Home -> mentor name), this fills the gap.
  const breadcrumbJsonLd: Record<string, unknown> = {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: [
      {
        '@type': 'ListItem',
        position: 1,
        name: 'Home',
        item: constants.BASE_URL,
      },
      {
        '@type': 'ListItem',
        position: 2,
        name: 'All mentors',
        item: `${constants.BASE_URL}mentors`,
      },
    ],
  }

  return (
    <>
      <Head>
        <title>{pageTitle('All mentors')}</title>
        <script {...jsonLdScriptProps(mentorsJsonLd)} />
        <script {...jsonLdScriptProps(breadcrumbJsonLd)} />
      </Head>

      <MetaHeader
        customTitle="All mentors"
        customDescription="Every mentor on OpenMentor, listed alphabetically by name with their job title and company — browse the full directory and open any profile."
      />

      <NavHeader />

      <Section className="border-b border-line bg-surface" id="header">
        <div className="mx-auto max-w-[720px] py-4 text-center sm:py-8">
          <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · Directory</p>
          <h1 className="my-0 mt-3 text-3xl sm:text-[40px]">All mentors</h1>
          <p className="mb-0 mt-3 text-[15px] text-ink-soft">
            Every one of our {mentors.length.toLocaleString('en-US')} mentors, alphabetically —
            pick a name to open their full profile.
          </p>
        </div>
      </Section>

      <main className="mx-auto w-full max-w-[880px] px-5 pb-16 pt-8 sm:pb-24 sm:pt-12">
        <ul className="grid grid-cols-1 gap-x-10 sm:grid-cols-2">
          {mentors.map((mentor) => (
            <li key={mentor.id} className="border-b border-line py-3">
              {/* Plain link text, not a heading: an h2 per row would give
                  hundreds of headings and wreck the page's document outline. */}
              <Link href={`/mentor/${mentor.slug}`} className="group block">
                <span className="block text-[15px] font-bold text-ink group-hover:text-brand-cobalt">
                  {mentor.name}
                </span>
                <span className="mt-0.5 block text-[13px] text-ink-soft">
                  {mentor.job} · {mentor.workplace}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </main>

      <Footer />
    </>
  )
}
