import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('Privacy page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'privacy')

export default function Privacy(): JSX.Element {
  const title = 'Privacy Policy | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Privacy Policy" />
      </Head>

      <NavHeader />

      <Section className="bg-primary-100" id="header">
        <div className="text-center py-14 lg:w-3/4 mx-auto">
          <h1>Privacy Policy</h1>
        </div>
      </Section>

      <Section className="bg-white py-12">
        <div className="prose max-w-4xl mx-auto px-4">
          <p className="text-center text-lg">for the OpenMentor.io service</p>

          <p>
            <strong>Last updated:</strong> 9 July 2026
          </p>

          <p>
            <strong>Data controller:</strong>
            <br />
            Georgiy Mogelashvili, an individual (sole operator of OpenMentor.io), based in the
            Netherlands.
          </p>

          <p>
            <strong>Website:</strong> <a href="https://openmentor.io">https://openmentor.io</a>
            <br />
            <strong>Privacy contact:</strong>{' '}
            <a href="mailto:privacy@openmentor.io">privacy@openmentor.io</a>
            <br />
            <strong>General contact:</strong>{' '}
            <a href="mailto:hello@openmentor.io">hello@openmentor.io</a>
          </p>

          <p>
            This Policy explains what personal data OpenMentor.io (&ldquo;OpenMentor&rdquo;,
            &ldquo;we&rdquo;, &ldquo;us&rdquo;) collects, why we collect it, who we share it with,
            and what rights you have. We process personal data in accordance with the EU General
            Data Protection Regulation (GDPR).
          </p>

          <hr />

          <h2>1. What OpenMentor does</h2>

          <p>
            OpenMentor is an online platform that connects mentors with people looking for
            mentorship (mentees). Mentors publish a public profile; mentees send contact requests to
            a mentor of their choice. We act as a technical intermediary — sessions, payments, and
            any further communication happen directly between the mentor and the mentee.
          </p>

          <hr />

          <h2>2. What data we collect</h2>

          <h3>2.1. Mentor profiles</h3>

          <p>When you register as a mentor and use the service, we process:</p>

          <ul>
            <li>name or display name;</li>
            <li>email address;</li>
            <li>photo (avatar);</li>
            <li>job title, workplace, experience, specialization, and profile description;</li>
            <li>session price information;</li>
            <li>
              links to external resources and an optional free-text &ldquo;preferred
              contact&rdquo; (any contact detail you choose to share), if provided;
            </li>
            <li>calendar booking link, if provided;</li>
            <li>any other information you voluntarily add to your profile.</li>
          </ul>

          <p>
            Except for your email address, this information is{' '}
            <strong>published on the site</strong> and becomes publicly available at your own
            initiative when your profile is approved.
          </p>

          <h3>2.2. Mentee contact requests</h3>

          <p>When you send a request to a mentor, we process:</p>

          <ul>
            <li>name;</li>
            <li>email address;</li>
            <li>
              an optional free-text &ldquo;preferred contact&rdquo; (any contact detail you choose
              to share), if provided;
            </li>
            <li>the text of your message to the mentor;</li>
            <li>your self-assessed experience level, if provided.</li>
          </ul>

          <h3>2.3. Session reviews</h3>

          <p>
            If you leave a review after a session, we process the review text, ratings, and
            recommendation scores you submit.
          </p>

          <h3>2.4. Authentication data</h3>

          <p>
            We use passwordless magic-link authentication. For mentors and moderators we process
            email addresses, short-lived single-use login tokens, and session cookies. We do not
            store passwords.
          </p>

          <h3>2.5. Analytics data (only with your consent)</h3>

          <p>
            With your consent (see section 8), we collect pseudonymous product-analytics events and
            device/browser data to understand how the service is used and to improve it.
          </p>

          <h3>2.6. Anti-abuse and technical data</h3>

          <p>
            To protect the service against spam, bots, and abuse, we process IP addresses,
            rate-limiting counters, Cloudflare Turnstile signals on public forms, and standard
            server logs (IP address, user agent, request metadata). We also collect operational
            error and performance telemetry to keep the service reliable.
          </p>

          <hr />

          <h2>3. Purposes and lawful bases</h2>

          <ul>
            <li>
              <strong>Operating the mentor catalog and delivering requests</strong> (publishing
              mentor profiles, passing a mentee&rsquo;s request to the chosen mentor, session
              coordination, transactional emails) — performance of a contract (Art. 6(1)(b) GDPR).
            </li>
            <li>
              <strong>Authentication and account access</strong> (magic links, sessions) —
              performance of a contract (Art. 6(1)(b) GDPR).
            </li>
            <li>
              <strong>Session reviews and quality feedback</strong> — our legitimate interest in
              maintaining the quality of the platform (Art. 6(1)(f) GDPR).
            </li>
            <li>
              <strong>Product analytics</strong> — your consent (Art. 6(1)(a) GDPR), given via the
              cookie banner and revocable at any time.
            </li>
            <li>
              <strong>Security, anti-abuse, logging, and observability</strong> — our legitimate
              interest in keeping the service secure and reliable (Art. 6(1)(f) GDPR).
            </li>
          </ul>

          <p>We do not send marketing or advertising emails.</p>

          <hr />

          <h2>4. Who we share data with</h2>

          <p>
            4.1. When you send a request, your name, email, optional contact handle, and message are{' '}
            <strong>shared with the mentor you selected</strong>, solely so that the mentor can
            respond. This happens at your initiative and is the core function of the service.
          </p>

          <p>
            4.2. We use the following service providers (processors), which receive personal data
            only to the extent necessary to provide their services:
          </p>

          <ul>
            <li>Hetzner — hosting and infrastructure (Germany, EU);</li>
            <li>Amazon Web Services (SES) — transactional email delivery (EU region);</li>
            <li>Amazon Web Services (S3) — storage of profile images (EU region);</li>
            <li>PostHog — product analytics, EU cloud (only with your consent);</li>
            <li>
              Google (Tag Manager) — loading of analytics tags (only with your consent);
            </li>
            <li>Cloudflare (Turnstile) — spam and bot protection on public forms;</li>
            <li>Grafana Cloud — logging, monitoring, and error tracking;</li>
            <li>Cloudflare — DNS and content delivery.</li>
          </ul>

          <p>
            4.3. If a mentor embeds a scheduling calendar (e.g. Calendly, Koalendar, or CalendLab)
            on their contact page, any data you enter into that calendar widget goes directly to the
            respective calendar provider under that provider&rsquo;s own privacy policy. We
            recommend reviewing it before booking.
          </p>

          <p>
            4.4. We do not sell personal data and do not share it with other third parties except
            where required by law.
          </p>

          <hr />

          <h2>5. International transfers</h2>

          <p>
            Our primary hosting is located in the EU (Hetzner, Germany). Our AWS email and storage
            services are configured to use EU regions, and our PostHog analytics instance is
            configured to use PostHog&rsquo;s EU cloud. However, some of our providers (for example
            Cloudflare, Google, Grafana Labs, and Amazon Web Services as US-headquartered
            companies) may process limited data in the United States or other countries outside
            the EU/EEA. Where that happens, transfers are safeguarded by the European
            Commission&rsquo;s Standard Contractual Clauses (SCCs) or an applicable adequacy
            decision (such as the EU-U.S. Data Privacy Framework).
          </p>

          <hr />

          <h2>6. How long we keep data</h2>

          <p>
            We retain personal data for as long as it remains relevant to providing the service,
            and we delete it on request (see section 7). We do not currently apply fixed automatic
            expiry periods to service data; your erasure request is honoured at any time.
          </p>

          <ul>
            <li>
              <strong>Mentor profiles</strong> — kept while the profile is active on the platform;
              deleted upon your deletion request or account removal.
            </li>
            <li>
              <strong>Mentee contact requests and reviews</strong> — kept while they remain
              relevant to the service (for example, so the mentor can respond to an open request
              and so reviews remain attributable); deleted or anonymized upon your request.
            </li>
            <li>
              <strong>Login tokens</strong> — expire within minutes of issuance; sessions expire per
              their time-to-live.
            </li>
            <li>
              <strong>Server logs and observability data</strong> — approximately 30 days.
            </li>
            <li>
              <strong>Analytics data</strong> — per the retention settings of the analytics
              provider (PostHog).
            </li>
          </ul>

          <p>
            <strong>Backups:</strong> deleted data may persist in encrypted backups for a limited
            period and is removed permanently when those backups expire on their regular rotation
            schedule.
          </p>

          <hr />

          <h2>7. Your rights</h2>

          <p>Under the GDPR you have the right to:</p>

          <ul>
            <li>access the personal data we hold about you and receive a copy;</li>
            <li>have inaccurate data corrected;</li>
            <li>have your data erased (&ldquo;right to be forgotten&rdquo;);</li>
            <li>restrict or object to processing based on legitimate interests;</li>
            <li>receive your data in a portable format;</li>
            <li>withdraw consent at any time (for consent-based processing such as analytics);</li>
            <li>
              lodge a complaint with a data-protection supervisory authority. Our supervisory
              authority is the Dutch{' '}
              <a href="https://www.autoriteitpersoonsgegevens.nl/en">
                Autoriteit Persoonsgegevens
              </a>
              ; you may also complain to the supervisory authority of your own EU/EEA country.
            </li>
          </ul>

          <p>
            To exercise any of these rights, email{' '}
            <strong>
              <a href="mailto:privacy@openmentor.io">privacy@openmentor.io</a>
            </strong>
            . We may ask you to verify your identity to prevent unauthorized access to personal
            data. We respond within one month, as required by law.
          </p>

          <hr />

          <h2>8. Cookies and analytics</h2>

          <p>
            We use two kinds of cookies and similar technologies (such as browser local storage):
          </p>

          <ul>
            <li>
              <strong>Essential cookies</strong> — required for the service to work: session cookies
              for signed-in mentors and moderators, security and anti-abuse mechanisms (Cloudflare
              Turnstile on public forms), and the cookie/local-storage entry that remembers your
              consent choice (valid for 12 months). These are always on.
            </li>
            <li>
              <strong>Analytics cookies</strong> — set by PostHog (product analytics) and by tags
              loaded through Google Tag Manager, to help us understand product usage. These load{' '}
              <strong>only if you accept them in the consent banner</strong> shown on your first
              visit. You can decline them without losing any functionality, and you can withdraw
              your consent at any time by clearing the site&rsquo;s cookies and local storage in
              your browser (the banner will then appear again) or by contacting us.
            </li>
          </ul>

          <hr />

          <h2>9. Special categories of data</h2>

          <p>
            The service does not request and does not require special categories of personal data
            (such as health information, religious or political beliefs). The service is not
            intended for exchanging medical or health information. If you voluntarily include such
            information in free-text fields, it is considered provided at your own initiative;
            please refrain from doing so. Upon request, we will delete such information.
          </p>

          <hr />

          <h2>10. How we protect data</h2>

          <p>We apply appropriate technical and organizational measures, including:</p>

          <ul>
            <li>TLS encryption for all connections;</li>
            <li>passwordless magic-link authentication (no stored passwords);</li>
            <li>HttpOnly, Secure session cookies;</li>
            <li>rate limiting and Cloudflare Turnstile on public forms;</li>
            <li>restricted access to administrative interfaces and APIs;</li>
            <li>security monitoring and event logging;</li>
            <li>regular updates of software components and dependencies.</li>
          </ul>

          <hr />

          <h2>11. Changes to this Policy</h2>

          <p>
            We may update this Policy from time to time. The current version is always available at{' '}
            <Link href="/privacy">openmentor.io/privacy</Link>. For significant changes we will
            provide notice on the site.
          </p>
        </div>
      </Section>

      <Footer />
    </>
  )
}
