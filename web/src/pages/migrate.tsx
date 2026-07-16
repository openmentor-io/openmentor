import { useState } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { Turnstile } from '@marsidev/react-turnstile'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import type { ScheduleMigrationResponse } from '@/types'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('Migrate page rendered', {
    slug: context.query.slug ?? null,
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'migrate')

type SubmitState = 'idle' | 'loading' | 'scheduled' | 'already' | 'error'

export default function Migrate(): JSX.Element {
  const router = useRouter()
  const title = 'Migrate your profile | ' + seo.title

  const rawSlug = router.query.slug
  const slug = (Array.isArray(rawSlug) ? rawSlug[0] : rawSlug ?? '').trim().toLowerCase()

  const [captchaToken, setCaptchaToken] = useState('')
  const [state, setState] = useState<SubmitState>('idle')

  const handleSchedule = async (): Promise<void> => {
    setState('loading')
    try {
      const res = await fetch('/api/schedule-migration', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug, captchaToken }),
      })
      const data = (await res.json()) as ScheduleMigrationResponse
      if (data.success) {
        setState(data.alreadyScheduled ? 'already' : 'scheduled')
      } else {
        setState('error')
      }
    } catch {
      setState('error')
    }
  }

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Migrate your profile" />
        {/* Personal opt-in links only — keep out of search results */}
        <meta name="robots" content="noindex" />
      </Head>

      <NavHeader />

      <Section id="migrate">
        <div className="mx-auto max-w-2xl pt-4 sm:pt-10">
          <p className="meta-mono my-0 text-ink-mute">getmentor.dev → openmentor.io</p>
          <h1 className="mt-3 text-[26px] leading-[1.08] sm:text-[34px]">
            Move your getmentor.dev profile to OpenMentor
          </h1>

          {router.isReady && !slug && (
            <div className="mt-8 rounded-panel border border-line bg-white p-6 sm:p-8">
              <p className="mt-0">
                This page schedules the migration of a getmentor.dev mentor profile, but the link
                you opened doesn&apos;t point at one.
              </p>
              <p>
                Please use the personal link from the announcement message — it ends with{' '}
                <code>?slug=your-profile</code>. If you lost it, just write to us at{' '}
                <a className="link" href="mailto:hello@openmentor.io">
                  hello@openmentor.io
                </a>
                .
              </p>
            </div>
          )}

          {router.isReady && slug && (state === 'idle' || state === 'loading' || state === 'error') && (
            <div className="mt-8 rounded-panel border border-line bg-white p-6 sm:p-8">
              <p className="mt-0">You&apos;re about to migrate this getmentor.dev profile:</p>
              <p className="rounded-field border border-line bg-surface px-4 py-3 font-mono text-sm font-medium text-ink">
                getmentor.dev/mentor/{slug}
              </p>

              <p className="font-medium text-ink">Here&apos;s what will happen:</p>
              <ul className="list-disc space-y-1.5 pl-5 text-ink-soft">
                <li>Your profile text is translated to English (you can edit it afterwards).</li>
                <li>Your price is converted to US dollars.</li>
                <li>Your photo and tags come along.</li>
                <li>
                  The profile arrives <strong>approved but hidden</strong> — nobody sees it until
                  you log in and switch it on.
                </li>
                <li>We email you when it&apos;s ready (to the address from your profile).</li>
              </ul>
              <p className="text-sm text-ink-soft">
                Your getmentor.dev profile is not affected in any way. Migrations run in batches,
                so the email usually arrives within a day.
              </p>

              <Turnstile
                siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
                onSuccess={setCaptchaToken}
                onExpire={(): void => setCaptchaToken('')}
                options={{ language: 'en' }}
              />

              {state === 'error' && (
                <div className="mt-4 rounded-field border-[1.5px] border-danger/40 bg-danger/5 px-4 py-3 text-sm font-medium text-danger">
                  Something went wrong — please try again in a minute, or write to us at{' '}
                  <a className="link" href="mailto:hello@openmentor.io">
                    hello@openmentor.io
                  </a>
                  .
                </div>
              )}

              <button
                className="button mt-6"
                type="button"
                onClick={handleSchedule}
                disabled={state === 'loading' || !captchaToken}
              >
                {state === 'loading' ? 'Scheduling…' : 'Schedule migration'}
              </button>
            </div>
          )}

          {state === 'scheduled' && (
            <div className="mt-8 animate-rise-in rounded-panel border border-line bg-white p-6 shadow-card-hover sm:p-8">
              <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-brand-mint">
                <svg width="16" height="13" viewBox="0 0 11 9" fill="none" aria-hidden="true">
                  <path
                    d="M1 4.5L4 7.5L10 1"
                    stroke="#161A20"
                    strokeWidth="1.6"
                    strokeLinecap="round"
                  />
                </svg>
              </div>
              <p className="mt-0 font-display text-lg font-extrabold uppercase tracking-[-0.01em] text-ink">
                🎉 You&apos;re on the list!
              </p>
              <p>
                We&apos;ll migrate <span className="font-medium">{slug}</span> in the next batch
                and email you when your OpenMentor profile is ready to review. Nothing else to do
                for now.
              </p>
              <p className="text-sm text-ink-soft">
                Changed your mind? Just reply to the email when it arrives and we&apos;ll remove
                the profile.
              </p>
              <Link href="/" className="link">
                Browse OpenMentor in the meantime
              </Link>
            </div>
          )}

          {state === 'already' && (
            <div className="mt-8 animate-rise-in rounded-panel border border-line bg-white p-6 sm:p-8">
              <p className="mt-0 font-display text-lg font-extrabold uppercase tracking-[-0.01em] text-ink">
                Already scheduled ✔
              </p>
              <p>
                <span className="font-medium">{slug}</span> is already on the migration list (or
                migrated). You&apos;ll get — or already got — an email when the profile is ready.
              </p>
              <p className="text-sm text-ink-soft">
                No email after a couple of days? Write to us at{' '}
                <a className="link" href="mailto:hello@openmentor.io">
                  hello@openmentor.io
                </a>
                .
              </p>
            </div>
          )}
        </div>
      </Section>

      <Footer />
    </>
  )
}
