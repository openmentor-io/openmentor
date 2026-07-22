import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('About page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'about')

/** Manifesto section: display heading + quiet body copy. */
function Block({ title, children }: { title: string; children: React.ReactNode }): JSX.Element {
  return (
    <section className="mt-10 first:mt-0 sm:mt-12">
      <h2 className="my-0 text-xl sm:text-2xl">{title}</h2>
      <div className="mt-3 flex flex-col gap-3 text-[15px] leading-[1.65] text-ink-soft [&_a]:font-medium [&_a]:text-brand-cobalt [&_a]:underline [&_a]:decoration-brand-cobalt/30 [&_a]:underline-offset-2 [&_a:hover]:decoration-brand-cobalt">
        {children}
      </div>
    </section>
  )
}

export default function About(): JSX.Element {
  const title = 'About | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="About OpenMentor" />
      </Head>

      <NavHeader />

      <Section className="border-b border-line bg-surface" id="header">
        <div className="mx-auto max-w-[720px] py-4 text-center sm:py-8">
          <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · About</p>
          <h1 className="my-0 mt-3 text-3xl sm:text-[40px]">
            People helping people <span className="text-brand-cobalt">grow</span>
          </h1>
          <p className="mb-0 mt-3 text-[15px] text-ink-soft">
            What OpenMentor is, why it&rsquo;s free, and why it stays out of your way.
          </p>
        </div>
      </Section>

      <main className="mx-auto w-full max-w-[720px] px-5 pb-16 pt-8 sm:pb-24 sm:pt-12">
        <Block title="Why this exists">
          <p className="my-0">
            Almost everyone who grew in tech can point at a person: someone who reviewed their CV,
            walked them through a hard decision, or simply said &ldquo;you&rsquo;re ready,
            apply&rdquo; at the right moment. That kind of help changes careers — and it
            shouldn&rsquo;t depend on who you happen to know or what you can afford.
          </p>
          <p className="my-0">
            OpenMentor makes those conversations easy to start. Practitioners put up a profile;
            anyone can browse them, no account needed, and send a request. That&rsquo;s the whole
            product.
          </p>
        </Block>

        <Block title="Community first">
          <p className="my-0">
            This is community help, organized — not a marketplace. There are no ads, no commission,
            and no premium tier, and there never will be. Mentors who want to give their time for
            free are the heart of this place: &ldquo;Free&rdquo; is a first-class price here, worn
            proudly on the mentor card. Mentors who charge set their own price and keep all of it.
          </p>
          <p className="my-0">
            The running costs — servers and email, essentially — are covered by{' '}
            <Link href="/donate">donations</Link> from people who find the platform useful.
            That&rsquo;s the entire business model, and it&rsquo;s enough.
          </p>
        </Block>

        <Block title="Lightweight by design">
          <p className="my-0">
            Our job is to make the connection, then get out of the way. No chat system, no booking
            engine, no escrow, no invoicing. A mentee writes what they need; the mentor replies
            directly; from there it&rsquo;s two people talking — over email, a call, whatever works.
          </p>
          <p className="my-0">
            Payments follow the same principle: if a session is paid, mentor and mentee arrange it
            between themselves. That means zero commission and your own terms — and it also means
            the finance part is yours to handle. We think that trade is worth it: no barriers, no
            bureaucracy, quick human connections.
          </p>
        </Block>

        <Block title="Real practitioners">
          <p className="my-0">
            The mentors here are working professionals sharing genuine experience in the areas they
            actually practice — code, design, product, data, management, careers. No gurus, no life
            coaches, no promises of a 10x career in 30 days. Profiles are reviewed by humans before
            they appear, and the bar is simple: real practice, a specific area of help, plain
            language.
          </p>
          <p className="my-0">
            Sound like you? <Link href="/bementor">Become a mentor</Link> — free mentoring is warmly
            encouraged.
          </p>
        </Block>

        <Block title="Open by nature">
          <p className="my-0">
            OpenMentor is a community project, and the platform itself is{' '}
            <a
              href="https://github.com/openmentor-io/openmentor"
              target="_blank"
              rel="noopener noreferrer"
            >
              open source
            </a>{' '}
            (AGPL) — the catalog, the dashboard, this very page. It continues the spirit of{' '}
            <a href="https://getmentor.dev" target="_blank" rel="noopener noreferrer">
              getmentor.dev
            </a>
            , the Russian-language community that proved thousands of mentoring sessions can happen
            on goodwill alone.
          </p>
          <p className="my-0">
            Questions, ideas, or something broken? Write to{' '}
            <a href="mailto:hello@openmentor.io">hello@openmentor.io</a> — a human reads every
            email. More practical detail lives in the <Link href="/faq">FAQ</Link>.
          </p>
        </Block>

        {/* ── CTA band ─────────────────────────────────────────────────── */}
        <div className="mt-12 flex flex-col items-center gap-3 rounded-panel bg-pastel-sky-grad px-5 py-7 text-center sm:mt-14 sm:px-8 sm:py-9">
          <h2 className="my-0 text-xl sm:text-2xl">Meet someone who&rsquo;s been there</h2>
          <div className="mt-2 flex flex-col gap-2.5 sm:flex-row">
            <Link href="/" className="button px-[26px] py-[13px] text-[15px]">
              Find a mentor
            </Link>
            <Link href="/bementor" className="button-secondary px-[26px] py-[13px] text-[15px]">
              Become a mentor
            </Link>
          </div>
        </div>
      </main>

      <Footer />
    </>
  )
}
