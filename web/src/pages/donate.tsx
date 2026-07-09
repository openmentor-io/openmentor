import Head from 'next/head'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import donates from '@/config/donates'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('Donate page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'donate')

export default function Donate(): JSX.Element {
  const title = 'Support us | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Support us" />
      </Head>

      <NavHeader />

      <Section className="bg-primary-100" id="header">
        <div className="text-center pt-14 lg:w-1/2 mx-auto">
          <h1>🍩 Support us</h1>

          <p className="pt-6">
            We want mentorship to be accessible to everyone, so OpenMentor is a non-commercial
            project. We charge nothing — not from mentors and not from mentees.
          </p>
          <p>
            If you like what we do, you can support us with a small tip. It helps us pay for
            servers, hosting, and everything else that keeps the site running 🍩
          </p>
        </div>
      </Section>

      <Section id="how">
        <Section.Title>How to support</Section.Title>

        <div className="mx-auto max-w-2xl rounded-2xl bg-surface p-6 text-center sm:p-10">
          <p className="mt-0">
            The easiest way is to buy us a coffee on Ko-fi — a one-off tip, no account or
            subscription required.
          </p>

          <div className="flex flex-wrap justify-center items-center gap-4 py-6">
            {donates.map((donate) => (
              <a
                key={donate.name}
                className="button"
                href={donate.linkUrl}
                target="_blank"
                rel="noreferrer"
              >
                ☕ {donate.description}
              </a>
            ))}
          </div>

          <p className="mb-0">
            If that doesn&apos;t work for you —{' '}
            <a className="link" href="mailto:hello@openmentor.io">
              drop us an email
            </a>
            . We&apos;ll figure something out.
          </p>
        </div>
      </Section>

      <Footer />
    </>
  )
}
