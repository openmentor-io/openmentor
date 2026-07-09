import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('Terms page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'terms')

export default function Terms(): JSX.Element {
  const title = 'Terms of Service | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Terms of Service" />
      </Head>

      <NavHeader />

      <div className="bg-yellow-100 border-b border-yellow-300 text-yellow-900 text-center text-sm py-2 px-4">
        DRAFT — pending legal review.
      </div>

      <Section className="bg-primary-100" id="header">
        <div className="text-center py-14 lg:w-3/4 mx-auto">
          <h1>Terms of Service</h1>
        </div>
      </Section>

      <Section className="bg-white py-12">
        <div className="prose max-w-4xl mx-auto px-4">
          <p className="text-center text-lg">for the OpenMentor.io service</p>

          <p>
            <strong>Last updated:</strong> 7 July 2026 (draft)
          </p>

          <p>
            These Terms of Service (&ldquo;Terms&rdquo;) govern your use of the OpenMentor.io
            website and service (&ldquo;OpenMentor&rdquo;, the &ldquo;Service&rdquo;,
            &ldquo;we&rdquo;, &ldquo;us&rdquo;). By using the Service, you agree to these Terms. If
            you do not agree, please do not use the Service.
          </p>

          <hr />

          <h2>1. What the Service is</h2>

          <p>
            OpenMentor is an online platform that provides the technical means for establishing
            contact between experts (&ldquo;mentors&rdquo;) and people seeking mentorship or advice
            (&ldquo;mentees&rdquo;). Mentors publish public profiles; mentees browse the catalog and
            send contact requests to a mentor of their choice.
          </p>

          <p>
            OpenMentor is a <strong>technical intermediary only</strong>. We are not a party to any
            agreement, arrangement, or relationship between a mentor and a mentee. We do not act as
            an employer, agent, representative, or guarantor of either party, and we do not control
            the content, conduct, or outcome of their interactions.
          </p>

          <h2>2. No payment processing</h2>

          <p>
            OpenMentor does not process payments and does not take a commission. Any prices shown in
            mentor profiles are set by the mentors themselves. Payment for sessions, if any, is
            agreed and settled <strong>directly between the mentor and the mentee</strong>, outside
            the Service. We are not responsible for payment disputes, refunds, or the fulfilment of
            any paid or unpaid arrangement.
          </p>

          <h2>3. Profiles and accuracy of information</h2>

          <p>
            Information published by mentors in their profiles (including experience,
            qualifications, expertise, and achievements) is provided by the mentors themselves and{' '}
            <strong>is not verified by the Service</strong>. It is published &ldquo;as is&rdquo; and
            is for informational purposes only. We do not guarantee its accuracy, completeness, or
            currency.
          </p>

          <p>
            You are responsible for the accuracy of the information you submit and for keeping your
            account credentials (login email inbox) secure.
          </p>

          <h2>4. Code of conduct</h2>

          <p>When using the Service, you agree to:</p>

          <ul>
            <li>treat other users with respect and professionalism;</li>
            <li>
              not post or send content that is unlawful, harassing, discriminatory, defamatory,
              obscene, or misleading;
            </li>
            <li>not impersonate another person or misrepresent your qualifications;</li>
            <li>
              not use the Service for spam, advertising unrelated to mentorship, or bulk
              solicitation;
            </li>
            <li>
              not attempt to disrupt, probe, or circumvent the Service&rsquo;s security or rate
              limits;
            </li>
            <li>not scrape or harvest other users&rsquo; personal data;</li>
            <li>
              use contact details obtained through the Service only for arranging and conducting
              mentorship.
            </li>
          </ul>

          <h2>5. Moderation and removal</h2>

          <p>
            We review mentor applications before publication and may, at our sole discretion,
            decline, edit, hide, or remove any profile, review, or other content, and suspend or
            terminate access to the Service, in particular where these Terms are violated or where
            we consider content harmful to users or to the Service. We are not obliged to give prior
            notice, though we will normally explain moderation decisions on request.
          </p>

          <h2>6. Not for medical or health information</h2>

          <p>
            The Service is intended for professional and career mentorship. It is{' '}
            <strong>not intended for exchanging medical or health information</strong> and must not
            be used to seek, provide, or store medical advice or health data. Nothing on the Service
            constitutes medical, legal, financial, or other regulated professional advice.
          </p>

          <h2>7. Disclaimers</h2>

          <p>
            The Service is provided &ldquo;as is&rdquo; and &ldquo;as available&rdquo;, without
            warranties of any kind, express or implied. In particular:
          </p>

          <ul>
            <li>
              we give <strong>no guarantees</strong> regarding the quality, content, usefulness, or
              outcome of any consultation, mentorship, or other interaction between a mentor and a
              mentee;
            </li>
            <li>
              all claims, disagreements, and disputes between a mentor and a mentee are to be
              resolved <strong>exclusively between them</strong>, without the participation of the
              Service;
            </li>
            <li>
              we are not responsible for the content or safety of external websites linked from
              mentor profiles or user messages, nor for any interaction between the parties outside
              the platform;
            </li>
            <li>we do not warrant that the Service will be uninterrupted or error-free.</li>
          </ul>

          <h2>8. Limitation of liability</h2>

          <p>
            To the maximum extent permitted by applicable law, OpenMentor and its operator shall not
            be liable for any indirect, incidental, consequential, special, or punitive damages, or
            for any loss of profits, revenue, data, or reputation, arising out of or in connection
            with the use of the Service, including any consequences of actions or omissions of
            mentors or mentees during or following their interaction. Nothing in these Terms
            excludes liability that cannot be excluded under applicable law.
          </p>

          <h2>9. Privacy</h2>

          <p>
            Our processing of personal data is described in the{' '}
            <Link href="/privacy">Privacy Policy</Link>.
          </p>

          <h2>10. Changes to the Service and these Terms</h2>

          <p>
            We may modify or discontinue parts of the Service, and we may update these Terms from
            time to time. The current version is always available at{' '}
            <Link href="/terms">openmentor.io/terms</Link>. Continued use of the Service after
            changes take effect constitutes acceptance of the updated Terms.
          </p>

          <h2>11. Governing law</h2>

          <p>
            [TBD — pending D7: governing law and jurisdiction will be determined together with the
            controller/legal-entity decision.]
          </p>

          <h2>12. Contact</h2>

          <p>
            Questions about these Terms:{' '}
            <a href="mailto:hello@openmentor.io">hello@openmentor.io</a>. Privacy matters:{' '}
            <a href="mailto:privacy@openmentor.io">privacy@openmentor.io</a>.
          </p>
        </div>
      </Section>

      <Footer />
    </>
  )
}
