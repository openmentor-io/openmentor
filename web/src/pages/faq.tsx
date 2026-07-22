import Head from 'next/head'
import Link from 'next/link'
import type { GetServerSideProps } from 'next'
import type { ReactNode } from 'react'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('FAQ page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'faq')

/**
 * One question + answer. Questions are h3 (Inter bold, not display CAPS —
 * full-sentence questions read better in mixed case) with a self-anchor so
 * individual answers can be linked directly (e.g. /faq#payments-mentor).
 */
function QA({ id, q, children }: { id: string; q: string; children: ReactNode }): JSX.Element {
  return (
    <div id={id} className="scroll-mt-24 border-b border-line py-6 last:border-b-0">
      <h3 className="my-0 text-[17px] font-bold leading-[1.35] tracking-[-0.01em] text-ink">
        <a href={`#${id}`} className="hover:text-brand-cobalt">
          {q}
        </a>
      </h3>
      <div className="mt-2.5 flex flex-col gap-2.5 text-[15px] leading-[1.6] text-ink-soft [&_a]:font-medium [&_a]:text-brand-cobalt [&_a]:underline [&_a]:decoration-brand-cobalt/30 [&_a]:underline-offset-2 [&_a:hover]:decoration-brand-cobalt">
        {children}
      </div>
    </div>
  )
}

export default function Faq(): JSX.Element {
  const title = 'FAQ | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Frequently asked questions" />
      </Head>

      <NavHeader />

      <Section className="border-b border-line bg-surface" id="header">
        <div className="mx-auto max-w-[720px] py-4 text-center sm:py-8">
          <p className="meta-mono my-0 text-ink-mute">OpenMentor.io · Help</p>
          <h1 className="my-0 mt-3 text-3xl sm:text-[40px]">Frequently asked questions</h1>
          <p className="mb-0 mt-3 text-[15px] text-ink-soft">
            How OpenMentor works — and why it works this way. The short version: we connect you and
            get out of the way.
          </p>
          <div className="mt-5 flex justify-center gap-2.5">
            <a href="#mentees" className="button-secondary px-5 py-2.5 text-sm">
              For mentees
            </a>
            <a href="#mentors" className="button-secondary px-5 py-2.5 text-sm">
              For mentors
            </a>
          </div>
        </div>
      </Section>

      <main className="mx-auto w-full max-w-[720px] px-5 pb-16 pt-4 sm:pb-24 sm:pt-8">
        {/* ── For mentees ──────────────────────────────────────────────── */}
        <h2 id="mentees" className="mb-1 mt-8 scroll-mt-24 text-2xl sm:text-[28px]">
          For mentees
        </h2>

        <QA id="find-a-mentor" q="How do I find and contact a mentor?">
          <p className="my-0">
            Browse the <Link href="/">catalog</Link> — no account needed. Filter by topic, search by
            role, skill, or company, and read full profiles. When someone looks right, hit
            &ldquo;Contact&rdquo; on their profile and describe what you want to work on. The mentor
            gets your request and replies directly, usually within a couple of days.
          </p>
          <p className="my-0">
            There is no chat system or booking engine in between — from the first reply onward you
            talk to each other like two humans, over email or whatever channel you agree on.
          </p>
        </QA>

        <QA id="cost" q="How much does a session cost?">
          <p className="my-0">
            Every mentor sets their own price; you&rsquo;ll see it on their card and profile. Many
            mentor <strong>for free</strong> — community help is what this platform was built for.
            Others list a per-session price or say &ldquo;negotiable&rdquo;, in which case you
            simply agree on it together.
          </p>
        </QA>

        <QA id="payments" q="How do payments work?">
          <p className="my-0">
            They don&rsquo;t go through us — and that&rsquo;s by design. If a mentor charges, you
            two agree on the amount and payment method directly: bank transfer, PayPal, whatever
            suits you both. OpenMentor never touches the money and never takes a commission, so 100%
            of what you pay goes to your mentor.
          </p>
          <p className="my-0">
            The honest flip side: because we don&rsquo;t process payments, we can&rsquo;t mediate
            refunds or payment disputes. Settle the terms before the session — most mentors are
            happy to.
          </p>
        </QA>

        <QA id="what-to-write" q="What should I write in my request?">
          <p className="my-0">
            Be specific. Where you are now, where you want to get, and what you&rsquo;ve already
            tried. &ldquo;I&rsquo;m a mid-level backend engineer preparing for staff-level system
            design interviews&rdquo; gets a much better answer than &ldquo;I&rsquo;m looking for a
            mentor&rdquo;. Mentors are volunteers giving you their time — a concrete ask shows
            you&rsquo;ll use it well.
          </p>
        </QA>

        <QA id="no-reply" q="What if a mentor doesn't reply?">
          <p className="my-0">
            Mentors are practitioners with day jobs, so give it a few days. We remind them about
            waiting requests automatically. If a week or so passes with no reply, send a request to
            another mentor — and profiles that consistently leave requests unanswered are hidden
            from the catalog automatically, so the catalog stays honest.
          </p>
        </QA>

        <QA id="reviews" q="Can I leave a review?">
          <p className="my-0">
            Yes. When your mentor marks the session as finished, you&rsquo;ll get an email inviting
            you to leave a review. Reviews appear on the mentor&rsquo;s profile and help the next
            mentee choose.
          </p>
        </QA>

        <QA id="really-free" q="Is this really free? What's the catch?">
          <p className="my-0">
            There isn&rsquo;t one. OpenMentor is a community project: no ads, no commission, no
            premium tier. Browsing and contacting mentors costs nothing, and many mentors give their
            time for free. The platform runs on <Link href="/donate">donations</Link> that cover
            servers and email — that&rsquo;s the whole business model.
          </p>
        </QA>

        {/* ── For mentors ──────────────────────────────────────────────── */}
        <h2 id="mentors" className="mb-1 mt-12 scroll-mt-24 text-2xl sm:text-[28px]">
          For mentors
        </h2>

        <QA id="become-a-mentor" q="How do I become a mentor?">
          <p className="my-0">
            Fill in the <Link href="/bementor">application form</Link>, then confirm your email —
            your profile goes to review only after you click the link we send. Reviews are done by
            humans and usually take about a week, often less. Once approved, your profile appears in
            the catalog and you&rsquo;ll get an email with your public link.
          </p>
        </QA>

        <QA id="good-mentor" q="Who makes a good mentor here?">
          <p className="my-0">
            Someone who does the work they mentor in. You don&rsquo;t need to be famous, run a blog,
            or have twenty years of experience — you need real, current practice in a specific area:
            code, design, product, data, management, career moves.
          </p>
          <p className="my-0">
            Good profiles name who they can help (&ldquo;juniors switching into backend&rdquo;,
            &ldquo;engineers becoming leads&rdquo;) and speak plainly: &ldquo;I&rsquo;ll share how I
            approached this&rdquo; rather than &ldquo;I&rsquo;ll 10x your career&rdquo;. This is a
            place for practice, specifics, and genuine human involvement — not for gurus, life
            coaches, or funnels to a paid course.
          </p>
        </QA>

        <QA id="mentor-for-free" q="Do I have to charge? Can I mentor for free?">
          <p className="my-0">
            You never have to charge — and mentoring for free is warmly encouraged. Community help
            is the reason OpenMentor exists, and &ldquo;Free&rdquo; is a first-class price here,
            shown proudly on your card. Many of our mentors run free sessions; some charge for
            regular engagements and keep first sessions free. Any arrangement is fine — it&rsquo;s
            your time.
          </p>
        </QA>

        <QA id="payments-mentor" q="How do payments work if I do charge?">
          <p className="my-0">
            Directly between you and your mentee. You pick the method — invoice, PayPal, bank
            transfer — and keep 100% of what you charge: OpenMentor takes no commission and never
            handles the money.
          </p>
          <p className="my-0">
            The trade-off, stated honestly: you also handle it yourself — payment method, no-shows,
            and any paperwork your country requires. We keep the platform free of financial
            bureaucracy so the connection stays lightweight; in exchange, the finance part is yours.
          </p>
        </QA>

        <QA id="not-visible" q="Why isn't my profile visible right after I register?">
          <p className="my-0">
            Two steps happen first: you confirm your email (check your inbox — the review only
            starts after that), and then a human reviews your profile, which usually takes about a
            week. You&rsquo;ll get an email either way: approved and live, or returned with a note
            on what to improve.
          </p>
        </QA>

        <QA
          id="returned"
          q="My profile was &ldquo;returned for edits&rdquo; — what does that mean?"
        >
          <p className="my-0">
            Not a rejection — the reviewer left you a note about what&rsquo;s missing (usually: too
            vague about what you help with, or a missing photo). Log in, edit your profile, and
            resubmit. It goes back to the same review flow.
          </p>
        </QA>

        <QA id="login" q="How do I log in? I never set a password.">
          <p className="my-0">
            There are no passwords. Go to the <Link href="/mentor/login">login page</Link>, enter
            the email you registered with, and we&rsquo;ll send you a magic link that signs you in.
            Nothing to remember, nothing to leak.
          </p>
        </QA>

        <QA id="requests" q="How do requests work? What am I supposed to do with one?">
          <p className="my-0">
            When a mentee contacts you, the request lands in your dashboard and you get an email.
            Read what they need, then reach out directly via the contact they left. From there
            it&rsquo;s between the two of you — agree on format, time, and price.
          </p>
          <p className="my-0">
            One ask from us: keep the request status updated in your dashboard (contacted → working
            → done). It takes two clicks, stops our reminder emails, and when you mark a session
            done, the mentee gets invited to leave you a review.
          </p>
        </QA>

        <QA id="unanswered" q="What happens if I don't respond to requests?">
          <p className="my-0">
            We&rsquo;ll nudge you — a reminder when a request has waited a day, another when a
            session has stalled for several days. If requests sit unanswered for 30 days, your
            profile is automatically hidden from the catalog. Nothing is deleted: fix up the waiting
            requests and flip your visibility back on in the dashboard whenever you&rsquo;re ready.
          </p>
        </QA>

        <QA id="decline" q="Can I decline a request?">
          <p className="my-0">
            Of course — not every request is a fit, and a clear &ldquo;no&rdquo; beats silence.
            Decline from the dashboard with a reason, and the mentee gets a polite note so they can
            move on to another mentor.
          </p>
        </QA>

        <QA id="mentor-reviews" q="How do reviews work?">
          <p className="my-0">
            When you mark a session as done, the mentee receives an email inviting them to review
            it. Published reviews appear on your public profile.
          </p>
        </QA>

        <QA id="pause" q="How do I pause mentoring for a while?">
          <p className="my-0">
            Flip the visibility toggle in your dashboard. Your profile disappears from the catalog
            and the contact button is hidden, so no new requests arrive; the direct link keeps
            working. Flip it back any time — the change is instant, no re-review needed.
          </p>
        </QA>

        <QA id="calendar" q="Can I link my calendar?">
          <p className="my-0">
            Yes — add a booking link (Calendly, Koalendar, or any URL) to your profile and mentees
            can pick a slot directly. It&rsquo;s optional; plenty of mentors just agree on a time
            over email.
          </p>
        </QA>

        <QA id="account" q="How do I change my email or delete my account?">
          <p className="my-0">
            Write to <a href="mailto:hello@openmentor.io">hello@openmentor.io</a> and we&rsquo;ll
            sort it out. For deleting your account and personal data, see the{' '}
            <Link href="/privacy">privacy policy</Link> or email{' '}
            <a href="mailto:privacy@openmentor.io">privacy@openmentor.io</a> — profiles and data are
            removed on request.
          </p>
        </QA>

        <QA id="getmentor" q="I had a profile on getmentor.dev — do I need to register again?">
          <p className="my-0">
            No — you can move your existing profile over. Open{' '}
            <Link href="/migrate">openmentor.io/migrate</Link> with your getmentor slug and schedule
            the migration; we&rsquo;ll copy your profile (translated to English), keep it hidden
            until you&rsquo;ve reviewed it, and email you when it&rsquo;s ready. Prefer a fresh
            start? Just <Link href="/bementor">register anew</Link>.
          </p>
        </QA>

        <QA id="commission" q="Does OpenMentor take a cut?">
          <p className="my-0">
            No. Not from mentors, not from mentees, not ever — there is no payment flow to take a
            cut from. The platform is funded by <Link href="/donate">donations</Link> alone.
          </p>
        </QA>

        {/* ── Outro ────────────────────────────────────────────────────── */}
        <div className="mt-10 rounded-panel border border-line bg-surface px-5 py-5 text-center sm:px-8 sm:py-6">
          <p className="my-0 text-[15px] leading-[1.6] text-ink-soft">
            Something we didn&rsquo;t cover? Write to{' '}
            <a
              href="mailto:hello@openmentor.io"
              className="font-medium text-brand-cobalt underline decoration-brand-cobalt/30 underline-offset-2 hover:decoration-brand-cobalt"
            >
              hello@openmentor.io
            </a>{' '}
            — a human reads every email.
          </p>
        </div>
      </main>

      <Footer />
    </>
  )
}
