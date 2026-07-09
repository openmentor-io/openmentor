import Head from 'next/head'
import Link from 'next/link'
import { useEffect } from 'react'
import type { GetServerSideProps, InferGetServerSidePropsType } from 'next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import type { IconDefinition } from '@fortawesome/fontawesome-svg-core'
import { faIdBadge, faComments, faEdit } from '@fortawesome/free-solid-svg-icons'
import {
  MentorsFilters,
  MentorsList,
  MetaHeader,
  NavHeader,
  Section,
  useMentors,
  Footer,
} from '@/components'
import { getAllMentors } from '@/server/mentors-data'
import donates from '@/config/donates'
import analytics from '@/lib/analytics'
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

interface FeatureProps {
  icon: IconDefinition
  title: string
  text: string
  subline: string
}

function Feature(props: FeatureProps) {
  return (
    <div className="flex sm:w-1/2 lg:w-1/3 p-4">
      <div className="pr-4">
        <FontAwesomeIcon className="text-primary" icon={props.icon} size="2x" fixedWidth />
      </div>

      <div>
        <h3 className="text-xl font-semibold mb-4">{props.title}</h3>
        <div>{props.text}</div>
        <br />
        <div>{props.subline}</div>
      </div>
    </div>
  )
}

export default function Home({
  pageMentors,
}: InferGetServerSidePropsType<typeof getServerSideProps>): JSX.Element {
  const [mentors, searchInput, hasMoreMentors, setSearchInput, showMoreMentors, appliedFilters] =
    useMentors(pageMentors)

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

  return (
    <>
      <Head>
        <title>{seo.title}</title>
        <MetaHeader />
      </Head>

      <NavHeader searchValue={searchInput} onSearchChange={setSearchInput} />

      <Section id="header">
        {/* Mobile frame in the design left-aligns the hero; desktop centers it. */}
        <div className="pt-4 pb-2 text-left md:pt-12 md:pb-4 md:text-center">
          <h1 className="max-w-4xl text-4xl sm:text-5xl md:mx-auto md:text-6xl lg:text-7xl">
            Your mentorship journey
            <br />
            starts here
          </h1>

          <p className="mt-4 max-w-2xl text-gray-500 md:mx-auto md:mt-6">
            OpenMentor is an open community of tech mentors ready to share their knowledge and
            experience one on one.
          </p>
        </div>
      </Section>

      <Section id="list">
        <h2 className="sr-only">Our mentors</h2>

        <div className="mb-8">
          <MentorsFilters appliedFilters={appliedFilters} />
        </div>

        <MentorsList
          mentors={mentors}
          hasMore={hasMoreMentors}
          onClickMore={handleShowMoreMentors}
        />
      </Section>

      <Section id="howitworks">
        <Section.Title>How it works</Section.Title>

        <div className="flex flex-wrap">
          <Feature
            icon={faIdBadge}
            title="Pick a mentor"
            text="2000+ experts from top tech companies work with us. Choose the right person by specialty, years of experience, and session price."
            subline="We vet every mentor ourselves: no charlatans."
          />

          <Feature
            icon={faEdit}
            title="Reach out"
            text="Leave a request on the site. Describe what you need help with and what you'd like to get out of it."
            subline="Remember: a well-stated problem is half solved. The more detail, the better."
          />

          <Feature
            icon={faComments}
            title="Take it from there"
            text="We'll forward your request to the mentor. They'll review it and contact you to discuss the details and pick a time. Every mentor sets their own session price and duration."
            subline="We stay out of the way — the rest is up to you."
          />
        </div>
      </Section>

      <Section className="bg-gray-100" id="support">
        <Section.Title>Support the project</Section.Title>

        <div className="flex flex-wrap justify-center items-center">
          {donates.map((donate) => (
            <a
              key={donate.name}
              className="button m-2"
              href={donate.linkUrl}
              target="_blank"
              rel="noreferrer"
            >
              ☕ {donate.description}
            </a>
          ))}
        </div>

        <div className="text-center mt-4">
          <Link href="/donate" className="link">
            Why it matters
          </Link>
        </div>
      </Section>

      <Section id="donate">
        <Section.Title>🍩 Support us</Section.Title>

        <div className="text-center">
          <p>
            Finding a mentor is hard&nbsp;— if only because it&apos;s not obvious where to look. And
            if you&apos;re an expert, finding mentees is just as hard. This site was created as a
            place where people who need a mentor&apos;s help and experts ready to share their
            knowledge can find each other.
          </p>

          <p>
            Our main goal&nbsp;is to connect people and grow the community through new connections
            and knowledge sharing.
            <br />
            <strong className="text-primary">
              We charge no commission, no participation fees, and no other mandatory payments — not
              from mentors and not from mentees.
            </strong>
            <br />
            We believe that if this platform brings value to people, they&apos;ll want to thank us
            for it themselves.
          </p>

          <p>
            That&apos;s why you can donate whatever amount you like. It&apos;s easy —{' '}
            <Link href="/donate" className="link">
              here&apos;s how
            </Link>
            .
          </p>

          <Link href="/donate" className="button">
            Say thanks
          </Link>
        </div>
      </Section>

      <Section className="bg-gray-100" id="addyourown">
        <Section.Title>Become a mentor</Section.Title>

        <div className="text-center">
          <p>
            Do you have experience and want to share your knowledge and help others?{' '}
            <strong>Join our team of mentors!</strong>
          </p>

          <p>Fill out the form and we&apos;ll add you to the site.</p>

          <Link href="/bementor" className="button">
            Apply now
          </Link>
        </div>
      </Section>

      <Section id="faq">
        <Section.Title>FAQ</Section.Title>

        <div className="prose max-w-none">
          <h3>❓&nbsp;Why does this exist?</h3>
          <p>
            We see a huge need among today&apos;s professionals for mentors who can help them
            overcome challenges and teach them the finer points of the craft. This service is an
            attempt to build a community of mentors and mentees and make it easier for them to find
            each other.
          </p>

          <h3>📅&nbsp;I&apos;ve requested a session with a mentor. What now?</h3>
          <p>
            Great! As soon as you leave a mentorship request, we pass it on to the expert you chose.
            They&apos;ll review it within a couple of days. If the mentor decides they can help,
            they&apos;ll contact you directly to pick a time and a way to meet.
          </p>
          <p>
            Sometimes a mentor may decline a request. It doesn&apos;t mean you did anything wrong —
            they may simply lack the time or the specific expertise. In that case we&apos;ll be sure
            to let you know, so you can find another expert.
          </p>

          <h3>💶&nbsp;How much does it cost?</h3>
          <p>
            We want to build a community, so we&apos;d rather keep money out of the process. Still,
            we understand that an expert&apos;s time can be worth something. That&apos;s why every
            mentor sets their own session price, which we show on their card. The price is a
            guideline and is always discussed with the expert directly.
          </p>
          <p>
            Our platform takes absolutely nothing from that price. If you&apos;d like to support the
            project and thank us for our work, you can <Link href="/donate">make a donation</Link>.
          </p>

          <h3>🚫&nbsp;I couldn&apos;t find a mentor. What should I do?</h3>
          <p>
            It happens — don&apos;t be discouraged. You can share a link to this site with your
            network so more people learn about the platform and join as experts.
          </p>

          <h3>🙋‍♀️&nbsp;How do I become a mentor?</h3>
          <p>
            It&apos;s easy. Just <Link href="/bementor">apply here</Link> and we&apos;ll add you.
          </p>

          <h3>👋&nbsp;I have ideas. Where do I write?</h3>
          <p>
            Drop us <a href="mailto:hello@openmentor.io">an email</a> — we&apos;ll be happy to read
            and reply.
          </p>
        </div>
      </Section>

      <Footer />
    </>
  )
}
