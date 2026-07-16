import Head from 'next/head'
import { useState } from 'react'
import type { GetServerSideProps } from 'next'
import classNames from 'classnames'
import { Footer, MetaHeader, NavHeader } from '@/components'
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

const AMOUNTS = ['$3', '$10', '$25', 'Custom'] as const
type Amount = (typeof AMOUNTS)[number]

export default function Donate(): JSX.Element {
  const title = 'Support us | ' + seo.title
  const kofi = donates[0]
  const [amount, setAmount] = useState<Amount>('$10')

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Support us" />
      </Head>

      <NavHeader backLink={{ href: '/', label: 'Back to mentors' }} />

      <main className="mx-auto flex w-full max-w-[960px] flex-col items-center px-5 pb-16 pt-9 text-center sm:px-8 sm:pb-24 sm:pt-16">
        {/* Logomark echo (ring + cobalt stroke + mint node), animated once
            on load per design 05 MOTION: ring first, stroke slides in,
            mint node pops last. Reduced motion: static (global). */}
        <div aria-hidden="true" className="relative mb-[18px] h-[72px] w-[72px] sm:mb-6 sm:h-24 sm:w-24">
          <div className="m-[7px] h-[58px] w-[58px] animate-rise-in rounded-full border-8 border-brand-navy sm:m-[9px] sm:h-[78px] sm:w-[78px] sm:border-[10px]" />
          <div
            className="absolute -left-3 bottom-2.5 h-2 w-[34px] -rotate-[35deg] animate-rise-in rounded-full bg-brand-cobalt sm:-left-4 sm:bottom-3.5 sm:h-2.5 sm:w-11"
            style={{ animationDelay: '140ms' }}
          />
          <div
            className="absolute right-0.5 top-0.5 h-4 w-4 animate-[rise-in_300ms_cubic-bezier(.34,1.56,.64,1)_320ms_both] rounded-full bg-brand-mint sm:right-1 sm:top-1 sm:h-5 sm:w-5"
          />
        </div>

        <h1 className="my-0 text-[30px] leading-[1.05] tracking-[-0.02em] sm:text-5xl sm:leading-[1.02] sm:tracking-[-0.03em]">
          Keep mentorship
          <br className="hidden sm:block" /> <span className="text-brand-cobalt">free</span> for
          everyone
        </h1>

        <p className="mb-0 mt-3 max-w-[520px] text-sm leading-[1.55] text-ink-soft sm:mt-[18px] sm:text-base sm:leading-[1.6]">
          OpenMentor takes zero commission and shows no ads. Servers, email delivery, and
          background-removal magic are paid for by people like you.
        </p>

        {/* ── Ko-fi card ──────────────────────────────────────────────── */}
        <div className="mt-6 flex w-full max-w-[520px] flex-col gap-3.5 rounded-panel border border-line bg-white p-5 shadow-card-hover sm:mt-9 sm:gap-[18px] sm:rounded-[18px] sm:p-7">
          <div className="flex items-center justify-between">
            <span className="font-display text-[15px] font-extrabold uppercase tracking-[0.02em] text-ink">
              Buy us a coffee
            </span>
            <span className="meta-mono text-ink-mute">Via Ko-fi</span>
          </div>

          <div className="flex justify-center gap-2" role="group" aria-label="Donation amount">
            {AMOUNTS.map((value) => {
              const selected = value === amount
              return (
                <button
                  key={value}
                  type="button"
                  aria-pressed={selected}
                  onClick={() => setAmount(value)}
                  className={classNames(
                    'flex-1 rounded-btn py-3 text-sm font-bold transition-colors duration-120 sm:flex-none sm:px-[22px] sm:text-[15px]',
                    selected
                      ? 'bg-brand-navy text-white shadow-btn'
                      : 'border-[1.5px] border-brand-cobalt/45 bg-white text-brand-navy hover:bg-brand-cobalt/[0.06]',
                    value === 'Custom' && 'hidden sm:block'
                  )}
                >
                  {value}
                </button>
              )
            })}
          </div>

          <a
            href={kofi.linkUrl}
            target="_blank"
            rel="noreferrer"
            className="button w-full py-4 text-[15px]"
          >
            {amount === 'Custom' ? 'Donate on Ko-fi ↗' : `Donate ${amount} on Ko-fi ↗`}
          </a>

          <span className="text-xs leading-[1.5] text-ink-soft">
            Opens ko-fi.com in a new tab · one-time or monthly · no account needed
          </span>
        </div>

        {/* ── Where the money goes ────────────────────────────────────── */}
        <div className="mt-6 grid w-full max-w-[760px] gap-3 sm:mt-9 sm:grid-cols-3 sm:gap-4">
          <div className="rounded-card border border-line p-[18px] text-left">
            <div className="font-name text-[22px] font-bold leading-tight text-brand-navy">
              $0
            </div>
            <div className="mt-1 text-[13px] leading-[1.5] text-ink-soft">
              Commission — mentors and mentees never pay us a cent
            </div>
          </div>
          <div className="rounded-card border border-line p-[18px] text-left">
            <div className="font-name text-[22px] font-bold leading-tight text-brand-navy">0</div>
            <div className="mt-1 text-[13px] leading-[1.5] text-ink-soft">
              Ads or premium tiers — donations are our only income
            </div>
          </div>
          <div className="rounded-card border border-line p-[18px] text-left">
            <div className="font-name text-[22px] font-bold leading-tight text-mint-ink">100%</div>
            <div className="mt-1 text-[13px] leading-[1.5] text-ink-soft">
              Of donations go to running costs — the team is volunteers
            </div>
          </div>
        </div>

        <p className="mb-0 mt-8 max-w-[520px] text-sm leading-[1.6] text-ink-soft sm:mt-10">
          If Ko-fi doesn&apos;t work for you —{' '}
          <a className="link" href="mailto:hello@openmentor.io">
            drop us an email
          </a>
          . We&apos;ll figure something out.
        </p>
      </main>

      <Footer />
    </>
  )
}
