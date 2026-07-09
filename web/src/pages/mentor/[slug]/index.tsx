import { useEffect } from 'react'
import Link from 'next/link'
import Head from 'next/head'
import Image from 'next/image'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { Footer, HtmlContent, MetaHeader, NavHeader, Section } from '@/components'
import { getOneMentorBySlug } from '@/server/mentors-data'
import seo from '@/config/seo'
import analytics from '@/lib/analytics'
import pluralize from '@/lib/pluralize'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
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

      <NavHeader />

      <Section id="body">
        {/* Two-column on desktop (photo/meta card + content), single column
            on mobile — redesign Phase B extension of the homepage language. */}
        <div className="mx-auto grid max-w-5xl gap-8 md:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] lg:gap-12">
          <aside>
            <div className="overflow-hidden rounded-2xl bg-surface md:sticky md:top-6">
              <div className="relative">
                <div className="aspect-w-1 aspect-h-1">
                  <Image
                    src={imageLoader({
                      src: mentor.slug,
                      quality: 'large',
                      version: updatedAtToVersion(mentor.updatedAt),
                    })}
                    alt={mentor.name}
                    fill
                    sizes="(max-width: 768px) 100vw, 40vw"
                    style={{ objectFit: 'cover' }}
                    priority
                    unoptimized
                  />
                </div>
              </div>

              <div className="p-6">
                <h1 className="text-2xl sm:text-3xl">{mentor.name}</h1>
                <div className="mt-1 text-ink-soft">
                  {mentor.job} @ {mentor.workplace}
                </div>

                <dl className="mt-5 grid grid-cols-[auto,1fr] gap-x-5 gap-y-1.5 text-sm">
                  <dt className="text-ink-soft">Experience</dt>
                  <dd className="font-medium">{mentor.experience} years</dd>

                  <dt className="text-ink-soft">Price per hour</dt>
                  <dd className="font-medium">{mentor.price}</dd>

                  {mentor.menteeCount > 0 && (
                    <>
                      <dt className="text-ink-soft">Helped</dt>
                      <dd className="font-medium">
                        {mentor.menteeCount} {pluralize(mentor.menteeCount, 'mentee')}
                      </dd>
                    </>
                  )}
                </dl>

                {!mentor.isVisible && (
                  <div className="mt-5 text-sm text-ink-soft">
                    This mentor is temporarily not accepting new requests.
                  </div>
                )}

                {mentor.isVisible && (
                  <Link
                    href={'/mentor/' + mentor.slug + '/contact'}
                    className="button mt-6 block w-full text-center"
                  >
                    Send a request
                  </Link>
                )}
              </div>
            </div>
          </aside>

          <div className="min-w-0">
            {mentor.tags.length > 0 && (
              <div className="mb-8 flex flex-wrap gap-2">
                {mentor.tags.map((tag) => (
                  <span
                    key={tag}
                    className="rounded-full bg-surface px-3.5 py-1.5 text-sm text-ink-soft"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            )}

            {mentor.about && (
              <section className="mb-10">
                <h2 className="mb-3 text-xl font-semibold tracking-tight">About me</h2>
                <div className="prose max-w-none">
                  <HtmlContent content={mentor.about} />
                </div>
              </section>
            )}

            {mentor.description && (
              <section className="mb-10">
                <h2 className="mb-3 text-xl font-semibold tracking-tight">How I can help</h2>
                <div className="prose max-w-none">
                  <HtmlContent content={mentor.description} />
                </div>
              </section>
            )}

            {mentor.competencies && (
              <section className="mb-10">
                <h2 className="mb-3 text-xl font-semibold tracking-tight">Skills</h2>
                <p className="text-ink-soft">{mentor.competencies}</p>
              </section>
            )}
          </div>
        </div>
      </Section>

      <Footer />
    </>
  )
}
