/**
 * Mentor Login Page (design 06)
 *
 * Passwordless authentication using email + magic link/token: centered
 * white card over the paper backdrop with ring echoes; the card content
 * swaps to a "check your inbox" state after the link is sent.
 */

import { useState, useEffect } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
import { useForm } from 'react-hook-form'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import classNames from 'classnames'
import { MentorAuthProvider, useMentorAuth } from '@/components/mentor-admin'
import analytics from '@/lib/analytics'

interface LoginFormData {
  email: string
}

function LoginForm(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading, requestLogin } = useMentorAuth()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitSuccess, setSubmitSuccess] = useState(false)
  const [sentEmail, setSentEmail] = useState('')
  const { expired, callback_error } = router.query

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginFormData>()

  // Redirect if already authenticated
  useEffect(() => {
    if (!authLoading && isAuthenticated) {
      router.replace('/mentor')
    }
  }, [authLoading, isAuthenticated, router])

  useEffect(() => {
    analytics.event(analytics.events.MENTOR_AUTH_LOGIN_REQUESTED, {
      outcome: 'login_page_viewed',
    })
  }, [])

  const onSubmit = async (data: LoginFormData): Promise<void> => {
    setIsSubmitting(true)
    setSubmitError(null)
    analytics.event(analytics.events.MENTOR_AUTH_LOGIN_REQUESTED, {
      outcome: 'submitted',
    })

    try {
      const result = await requestLogin(data.email)
      if (result.success) {
        setSentEmail(data.email)
        setSubmitSuccess(true)
        analytics.event(analytics.events.MENTOR_AUTH_LOGIN_REQUESTED, {
          outcome: 'success',
        })
      } else {
        setSubmitError(result.message || 'Something went wrong. Please try again.')
        analytics.event(analytics.events.MENTOR_AUTH_LOGIN_REQUESTED, {
          outcome: 'error',
          error_type: 'api_error',
        })
      }
    } catch {
      setSubmitError('Something went wrong. Please try again.')
      analytics.event(analytics.events.MENTOR_AUTH_LOGIN_REQUESTED, {
        outcome: 'error',
        error_type: 'network_error',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  // Show loading while checking auth
  if (authLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <div className="relative flex min-h-screen flex-col justify-center overflow-hidden bg-surface px-5 py-12">
      <Head>
        <title>Mentor login — openmentor.io</title>
      </Head>

      {/* Backdrop ring echoes */}
      <div
        aria-hidden="true"
        className="absolute -right-36 -top-36 h-[480px] w-[480px] rounded-full border-[56px] border-brand-navy/5"
      />
      <div
        aria-hidden="true"
        className="absolute -bottom-24 -left-20 h-[340px] w-[340px] rounded-full border-[44px] border-brand-mint/[0.07]"
      />

      <div className="relative mx-auto w-full max-w-[440px]">
        <div className="animate-rise-in rounded-[20px] border border-line bg-white p-7 shadow-[0_20px_50px_-24px_rgba(19,42,82,0.22)] sm:p-10">
          {/* Session expired message */}
          {expired === 'true' && (
            <div className="mb-5 rounded-field border border-line bg-surface p-3.5">
              <p className="my-0 text-sm text-ink">
                Your session has expired. Please sign in again.
              </p>
            </div>
          )}

          {/* Callback error message */}
          {callback_error && (
            <div className="mb-5 rounded-field border border-danger/40 bg-danger/5 p-3.5">
              <p className="my-0 text-sm text-danger">
                {callback_error === 'invalid_token'
                  ? 'The link is invalid or has expired. Please request a new one.'
                  : 'Something went wrong. Please try again.'}
              </p>
            </div>
          )}

          {submitSuccess ? (
            /* Sent state — card content swaps in place */
            <div className="flex flex-col items-center gap-5 text-center">
              {/* Logomark ring + mint check */}
              <div aria-hidden="true" className="relative h-[76px] w-[76px]">
                <div className="m-2 h-[60px] w-[60px] rounded-full border-8 border-brand-navy" />
                <div className="absolute right-0 top-1 flex h-5 w-5 items-center justify-center rounded-full bg-brand-mint">
                  <svg width="10" height="8" viewBox="0 0 11 9" fill="none">
                    <path
                      d="M1 4.5L4 7.5L10 1"
                      stroke="#fff"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </div>
              </div>

              <div>
                <h2 className="text-2xl tracking-[-0.02em] text-ink">Check your inbox</h2>
                <p className="my-0 mt-2.5 text-sm leading-relaxed text-ink-soft">
                  We sent a sign-in link to
                  <br />
                  <b className="text-ink">{sentEmail || 'your email'}</b>
                  <br />
                  Follow it to open your dashboard.
                </p>
              </div>

              <div className="flex w-full items-center justify-between rounded-field border border-line px-4 py-3">
                <span className="text-[13px] text-ink-soft">Didn&apos;t arrive?</span>
                <span className="text-[13px] font-semibold text-ink-soft">Check your spam</span>
              </div>

              <button
                onClick={() => setSubmitSuccess(false)}
                className="text-[13px] font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
              >
                Use a different email
              </button>
            </div>
          ) : (
            /* Email step */
            <div className="flex flex-col gap-5">
              <div className="flex items-center gap-2.5">
                <Image
                  src="/brand/logo/svg/logomark.svg"
                  width={38}
                  height={38}
                  alt=""
                  unoptimized
                />
                <span className="font-display text-lg font-extrabold uppercase tracking-[-0.02em] text-brand-navy">
                  openmentor<span className="text-brand-cobalt">.io</span>
                </span>
              </div>

              <div>
                <h1 className="text-[26px] tracking-[-0.02em] text-ink">Mentor login</h1>
                <p className="my-0 mt-2 text-sm leading-normal text-ink-soft">
                  No password. We&apos;ll email you a magic link that signs you in.
                </p>
              </div>

              <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-5" noValidate>
                <div className="flex flex-col gap-1.5">
                  <label htmlFor="email" className="text-[13px] font-semibold text-ink">
                    Email
                  </label>
                  <input
                    id="email"
                    type="email"
                    autoComplete="email"
                    placeholder="you@example.com"
                    {...register('email', {
                      required: 'Enter your email',
                      pattern: {
                        value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
                        message: 'Enter a valid email',
                      },
                    })}
                    className={classNames('field', errors.email && 'field-error animate-shake')}
                  />
                  {errors.email && (
                    <p className="my-0 text-xs font-medium text-danger" role="alert">
                      {errors.email.message}
                    </p>
                  )}
                </div>

                {submitError && (
                  <p className="my-0 text-sm font-medium text-danger" role="alert">
                    {submitError}
                  </p>
                )}

                <button type="submit" disabled={isSubmitting} className="button w-full text-[15px]">
                  {isSubmitting ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                      Sending...
                    </>
                  ) : (
                    'Email me a magic link'
                  )}
                </button>
              </form>

              <p className="my-0 text-center text-xs leading-relaxed text-ink-soft">
                Not a mentor yet?{' '}
                <Link href="/bementor" className="font-semibold text-brand-cobalt">
                  Create a profile
                </Link>
              </p>
            </div>
          )}

          {/* Help text */}
          <div className="mt-6 border-t border-line pt-5">
            <p className="my-0 text-center text-xs text-ink-soft">
              Use the email you provided when registering as a mentor. If you run into any issues,{' '}
              <a href="mailto:hello@openmentor.io" className="font-semibold text-brand-cobalt">
                write to us
              </a>
              .
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function MentorLoginPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <LoginForm />
    </MentorAuthProvider>
  )
}
