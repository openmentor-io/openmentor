import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { Footer, MentorsList, MetaHeader, NavHeader, Section } from '@/components'
import { getAllMentors } from '@/server/mentors-data'
import { pageTitle } from '@/config/seo'
import constants from '@/config/constants'
import { TAG_CATEGORIES, TAG_PAGES, tagBySlug, tagPath } from '@/config/tags'
import { jsonLdScriptProps } from '@/lib/json-ld'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import pluralize from '@/lib/pluralize'
import type { MentorListItem, MentorTag } from '@/types'

interface MentorTagPageProps {
  [key: string]: unknown
  tag: MentorTag
  mentors: MentorListItem[]
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
  const mentors = allMentors.filter((mentor) => mentor.tags.includes(tag))

  // A tag with zero live matches is thin/soft-404 content - keep it out of the index.
  if (mentors.length === 0) {
    logger.warn('Mentor tag page has no matching mentors', {
      tag,
      mentorCount: 0,
      ...getTraceContext(),
    })
    return { notFound: true }
  }

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

        <script {...jsonLdScriptProps(collectionJsonLd)} />
        <script {...jsonLdScriptProps(breadcrumbJsonLd)} />
      </Head>

      <MetaHeader customTitle={`${tag} mentors`} customDescription={metaDescription} />

      <NavHeader />

      <Section className="border-b border-line bg-surface" id="header">
        <div className="mx-auto max-w-[720px] py-4 text-center sm:py-8">
          <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · Topic</p>
          <h1 className="my-0 mt-3 text-3xl sm:text-[40px]">{tag} mentors</h1>
          <p className="mb-0 mt-3 text-[15px] text-ink-soft">{blurb}</p>
          <p className="meta-mono mb-0 mt-3 text-ink-mute">{countLabel}</p>
        </div>
      </Section>

      <main className="px-5 pb-16 pt-8 sm:px-8 sm:pb-24 sm:pt-12 lg:px-16">
        {/* MentorsList renders each card name as an h3, so without this the
            outline would jump h1 -> h3. Same sr-only pattern as the homepage. */}
        <h2 className="sr-only">{tag} mentors</h2>

        {/* Bounded by however many mentors carry this tag - a fraction of the
            catalog, unlike the directory page, so no field projection needed. */}
        <MentorsList mentors={mentors} hasMore={false} onClickMore={() => {}} />
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
