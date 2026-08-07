import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { Footer, MentorsList, MetaHeader, NavHeader, Section } from '@/components'
import { getAllMentors } from '@/server/mentors-data'
import { toCardItem } from '@/server/mentor-projection'
import { pageTitle } from '@/config/seo'
import constants from '@/config/constants'
import { TAG_CATEGORIES, TAG_PAGES, tagBySlug, tagPath } from '@/config/tags'
import { jsonLdScriptProps } from '@/lib/json-ld'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import pluralize from '@/lib/pluralize'
import type { MentorCardItem, MentorTag } from '@/types'

interface MentorTagPageProps {
  [key: string]: unknown
  tag: MentorTag
  /**
   * Projected to just the card fields. A broad tag can carry a large slice of
   * the catalog, and `drop_long_fields` clears only `about`/`description` — a
   * full MentorListItem would still ship `competencies` (up to 5,000 chars),
   * tags and counts into __NEXT_DATA__ that no card reads.
   */
  mentors: MentorCardItem[]
}

const _getServerSideProps: GetServerSideProps<MentorTagPageProps> = async (context) => {
  const tagParam = context.params?.tag
  const slug = Array.isArray(tagParam) ? tagParam[0] : tagParam
  const tag = slug ? tagBySlug(slug) : null

  // Unknown segment -> 404, not an empty page rendered for a tag that doesn't exist.
  if (!tag) {
    logger.warn('Unknown mentor tag slug requested', { slug, ...getTraceContext() })
    return { notFound: true }
  }

  const allMentors = await getAllMentors({ onlyVisible: true, drop_long_fields: true })

  // A tag with no live matches still RENDERS, carrying noindex (see below).
  // 404ing it instead would mean every surface that links a tag — the topic
  // index, the sibling block, profile chips, llms.txt — had to agree on which
  // tags are currently non-empty. Two of those cannot: llms.txt is a static
  // file, and a paused mentor's profile links their tags without fetching the
  // catalog. One fragile invariant across four surfaces is worse than a page
  // that says "nobody covers this yet" and is kept out of the index.
  const mentors: MentorCardItem[] = allMentors
    .filter((mentor) => mentor.tags.includes(tag))
    .map(toCardItem)

  logger.info('Mentor tag page rendered', {
    tag,
    mentorCount: mentors.length,
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: { tag, mentors },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'mentor-tag')

export default function MentorTagPage({
  tag,
  mentors,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  const { blurb } = TAG_PAGES[tag]
  const isEmpty = mentors.length === 0
  const countLabel = `${mentors.length} ${pluralize(mentors.length, 'mentor')}`
  const canonicalUrl = `${constants.BASE_URL}${tagPath(tag).replace(/^\//, '')}`
  // Blurb alone can run to ~140 chars, so the count suffix stays minimal to
  // hold the total under the ~160 char scraper truncation point.
  const metaDescription = `${blurb} ${countLabel}.`

  // Kept to @type/position/name/url per entry, same budget as the directory's
  // ItemList (see mentors/index.tsx) - this list is a fraction of the catalog.
  const collectionJsonLd: Record<string, unknown> = {
    '@context': 'https://schema.org',
    '@type': 'CollectionPage',
    name: `${tag} mentors`,
    description: metaDescription,
    url: canonicalUrl,
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

  const breadcrumbJsonLd: Record<string, unknown> = {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: [
      { '@type': 'ListItem', position: 1, name: 'Home', item: constants.BASE_URL },
      {
        '@type': 'ListItem',
        position: 2,
        name: 'All mentors',
        item: `${constants.BASE_URL}mentors`,
      },
      { '@type': 'ListItem', position: 3, name: `${tag} mentors`, item: canonicalUrl },
    ],
  }

  return (
    <>
      <Head>
        <title>{pageTitle(`${tag} mentors`)}</title>

        {/* No ItemList worth publishing when the list is empty, and the page
            is noindex in that state anyway. The breadcrumb still applies. */}
        {!isEmpty && <script {...jsonLdScriptProps(collectionJsonLd)} />}
        <script {...jsonLdScriptProps(breadcrumbJsonLd)} />
      </Head>

      {/* Reachable but not indexable while empty — thin content stays out of
          the index without any linking surface having to know it is empty. */}
      <MetaHeader
        customTitle={`${tag} mentors`}
        customDescription={metaDescription}
        noIndex={isEmpty}
      />

      <NavHeader />

      <Section className="border-b border-line bg-surface" id="header">
        <div className="mx-auto max-w-[720px] py-4 text-center sm:py-8">
          <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · Topic</p>
          <h1 className="my-0 mt-3 text-3xl sm:text-[40px]">{tag} mentors</h1>
          <p className="mb-0 mt-3 text-[15px] text-ink-soft">{blurb}</p>
          <p className="meta-mono mb-0 mt-3 text-ink-mute">
            {isEmpty ? 'No mentors yet' : countLabel}
          </p>
        </div>
      </Section>

      <main className="px-5 pb-16 pt-8 sm:px-8 sm:pb-24 sm:pt-12 lg:px-16">
        {/* MentorsList renders each card name as an h3, so without this the
            outline would jump h1 -> h3. Same sr-only pattern as the homepage. */}
        <h2 className="sr-only">{tag} mentors</h2>

        {isEmpty ? (
          <div className="mx-auto max-w-[520px] py-8 text-center">
            <p className="my-0 text-[15px] leading-[1.6] text-ink-soft">
              Nobody covers {tag} on OpenMentor yet. Browse the other topics below, or put
              yourself forward if this is your field.
            </p>
            <div className="mt-6 flex flex-col items-center justify-center gap-2.5 sm:flex-row">
              <Link href="/bementor" className="button px-[30px] py-[15px] text-[15px]">
                Become a mentor
              </Link>
              <Link href="/mentors" className="button-ghost px-[26px] py-[15px] text-[15px]">
                See all mentors
              </Link>
            </div>
          </div>
        ) : (
          <MentorsList mentors={mentors} hasMore={false} onClickMore={() => {}} />
        )}
      </main>

      {/* Link graph: every tag page links to every other tag plus the full
          directory, so the 30 topic pages aren't orphans reachable only from
          the sitemap. Grouped by the same taxonomy as the catalog filters. */}
      <section className="border-t border-line bg-surface px-5 py-10 sm:px-8 lg:px-16">
        <div className="mx-auto max-w-[1000px]">
          <h2 className="mb-4 text-lg font-bold text-ink">Browse other topics</h2>

          <div className="mb-5 flex flex-wrap gap-2">
            <Link
              href="/mentors"
              className="rounded-field border-[1.5px] border-brand-cobalt/45 bg-white px-[15px] py-[8.5px] text-[13px] font-semibold text-brand-navy hover:bg-brand-cobalt/[0.06]"
            >
              All mentors
            </Link>
          </div>

          {TAG_CATEGORIES.map((category) => {
            const categoryTags = category.tags.filter((t) => t !== tag)
            if (categoryTags.length === 0) return null

            return (
              <div key={category.label} className="mb-5 last:mb-0">
                <h3 className="meta-mono mb-2 text-ink-mute">{category.label}</h3>
                <div className="flex flex-wrap gap-2">
                  {categoryTags.map((t) => (
                    <Link
                      key={t}
                      href={tagPath(t)}
                      className="rounded-field border-[1.5px] border-brand-cobalt/45 bg-white px-[15px] py-[8.5px] text-[13px] font-semibold text-brand-navy hover:bg-brand-cobalt/[0.06]"
                    >
                      {t}
                    </Link>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </section>

      <Footer />
    </>
  )
}
