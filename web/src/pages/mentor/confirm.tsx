import { useEffect, useRef, useState } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'
import analytics from '@/lib/analytics'
import type { ConfirmMentorEmailResponse } from '@/types'

type ConfirmState =
  | 'confirming'
  | 'success'
  | 'already'
  | 'invalid'
  | 'expired'
  | 'resending'
  | 'resent'
  | 'error'

export default function ConfirmMentorEmail(): JSX.Element {
  const router = useRouter()
  const title = 'Confirm your email | ' + seo.title

  const rawToken = router.query.token
  const token = (Array.isArray(rawToken) ? rawToken[0] : rawToken ?? '').trim()

  const [state, setState] = useState<ConfirmState>('confirming')
  const confirmStarted = useRef(false)

  // SECURITY (M10): strip the one-time confirmation token from the address bar
  // so it isn't captured by telemetry or leaked via referrer. router.query.token
  // (and the derived `token`) stay intact because replaceState doesn't touch
  // Next's router state.
  useEffect(() => {
    if (!router.isReady) return
    if (typeof window !== 'undefined' && window.location.search) {
      window.history.replaceState(null, '', window.location.pathname)
    }
  }, [router.isReady])

  useEffect(() => {
    if (!router.isReady || confirmStarted.current) {
      return
    }
    confirmStarted.current = true

    // Page-view only — confirmation outcomes are tracked by the backend.
    analytics.event(analytics.events.MENTOR_CONFIRM_PAGE_VIEWED, {
      has_token: token.length > 0,
    })

    if (!token) {
      setState('invalid')
      return
    }

    const confirm = async (): Promise<void> => {
      try {
        const res = await fetch('/api/mentor/confirm', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token }),
        })
        const data = (await res.json()) as ConfirmMentorEmailResponse
        if (data.success) {
          setState(data.already ? 'already' : 'success')
        } else if (res.status === 410 || data.code === 'token_expired') {
          setState('expired')
        } else if (res.status === 400 || data.code === 'invalid_token') {
          setState('invalid')
        } else {
          setState('error')
        }
      } catch {
        setState('error')
      }
    }
    void confirm()
  }, [router.isReady, token])

  const handleResend = async (): Promise<void> => {
    setState('resending')
    try {
      const res = await fetch('/api/mentor/confirm-resend', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      })
      const data = (await res.json()) as ConfirmMentorEmailResponse
      if (data.success) {
        setState(data.already ? 'already' : 'resent')
      } else {
        setState('error')
      }
    } catch {
      setState('error')
    }
  }

  const supportLink = (
    <a className="link" href="mailto:hello@openmentor.io">
      hello@openmentor.io
    </a>
  )

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Confirm your email" />
        {/* Personal confirmation links only — keep out of search results */}
        <meta name="robots" content="noindex" />
      </Head>

      <NavHeader />

      <Section id="confirm">
        <div className="mx-auto max-w-2xl pt-10">
          <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">Confirm your email</h1>

          {state === 'confirming' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">Confirming…</p>
              <p className="text-ink-soft">Hold on a second while we check your link.</p>
            </div>
          )}

          {state === 'success' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">
                🎉 Profile submitted for review — we&apos;ll email you
              </p>
              <p>
                Thanks for confirming your email. Our moderators will review your application and
                you&apos;ll get an email as soon as it&apos;s done. Nothing else to do for now.
              </p>
              <Link href="/" className="link">
                Browse OpenMentor in the meantime
              </Link>
            </div>
          )}

          {state === 'already' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">Already confirmed ✔</p>
              <p>
                Your email is already confirmed and your application is with our moderators (or
                further along). We&apos;ll email you about the outcome — no need to do anything
                else.
              </p>
            </div>
          )}

          {state === 'invalid' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">This link doesn&apos;t work</p>
              <p>
                The confirmation link is invalid or was already used. Please open the most recent
                confirmation email we sent you, or write to us at {supportLink} and we&apos;ll sort
                it out.
              </p>
            </div>
          )}

          {(state === 'expired' || state === 'resending') && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">This link has expired</p>
              <p>
                Confirmation links are valid for 24 hours. No worries — we can send you a fresh
                one to the same address.
              </p>
              <button
                className="button mt-4"
                type="button"
                onClick={handleResend}
                disabled={state === 'resending'}
              >
                {state === 'resending' ? 'Sending…' : 'Resend confirmation email'}
              </button>
            </div>
          )}

          {state === 'resent' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">📬 Fresh link sent</p>
              <p>
                Check your inbox — a new confirmation email is on its way. The link is valid for
                24 hours. If it doesn&apos;t arrive, please check your spam folder or write to us
                at {supportLink}.
              </p>
            </div>
          )}

          {state === 'error' && (
            <div className="mt-8 rounded-2xl bg-surface p-6 sm:p-8">
              <p className="mt-0 text-lg font-medium text-ink">Something went wrong</p>
              <p>
                Please try again in a minute, or write to us at {supportLink} and we&apos;ll sort
                it out.
              </p>
            </div>
          )}
        </div>
      </Section>

      <Footer />
    </>
  )
}
