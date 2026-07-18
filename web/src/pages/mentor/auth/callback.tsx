/**
 * Mentor Auth Callback Page
 *
 * Handles magic link verification and creates session.
 */

import { useEffect, useState } from 'react'
import Head from 'next/head'
import Image from 'next/image'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faCheckCircle, faTimesCircle } from '@fortawesome/free-solid-svg-icons'
import { MentorAuthProvider, useMentorAuth } from '@/components/mentor-admin'
import analytics from '@/lib/analytics'

type CallbackState = 'verifying' | 'success' | 'error'

function CallbackHandler(): JSX.Element {
  const router = useRouter()
  const { verifyLogin, isAuthenticated } = useMentorAuth()
  const [state, setState] = useState<CallbackState>('verifying')
  const [errorMessage, setErrorMessage] = useState<string>('')

  // SECURITY (M10): drop the one-time token from the address bar so it isn't
  // captured by later analytics/telemetry events or leaked via referrer.
  // history.replaceState (not router.replace) leaves router.query.token intact
  // for the verification effect below.
  useEffect(() => {
    if (!router.isReady) return
    if (typeof window !== 'undefined' && window.location.search) {
      window.history.replaceState(null, '', window.location.pathname)
    }
  }, [router.isReady])

  useEffect(() => {
    const token = router.query.token as string | undefined

    // If already authenticated, redirect to dashboard
    if (isAuthenticated) {
      router.replace('/mentor')
      return
    }

    // Wait for router to be ready
    if (!router.isReady) return

    // If no token, redirect to login with error
    if (!token) {
      analytics.event(analytics.events.MENTOR_AUTH_LOGIN_VERIFIED, {
        outcome: 'invalid_token',
      })
      router.replace('/mentor/login?callback_error=invalid_token')
      return
    }

    // Verify the token
    const verify = async (): Promise<void> => {
      try {
        const result = await verifyLogin(token)
        if (result.success) {
          setState('success')
          analytics.event(analytics.events.MENTOR_AUTH_LOGIN_VERIFIED, {
            outcome: 'success',
          })
          // Redirect to dashboard after brief success message
          setTimeout(() => {
            router.replace('/mentor')
          }, 1500)
        } else {
          setState('error')
          analytics.event(analytics.events.MENTOR_AUTH_LOGIN_VERIFIED, {
            outcome: 'error',
            error_type: 'invalid_token',
          })
          setErrorMessage(result.message || 'The link is invalid or has expired')
        }
      } catch {
        setState('error')
        analytics.event(analytics.events.MENTOR_AUTH_LOGIN_VERIFIED, {
          outcome: 'error',
          error_type: 'verification_failed',
        })
        setErrorMessage('Something went wrong while verifying the link')
      }
    }

    verify()
  }, [router, router.isReady, router.query.token, verifyLogin, isAuthenticated])

  return (
    <div className="flex min-h-screen flex-col justify-center bg-surface px-5 py-12">
      <Head>
        <title>Sign in — openmentor.io</title>
      </Head>

      <div className="mx-auto w-full max-w-md">
        <Link href="/" className="mb-8 flex justify-center">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={165}
            height={45}
            alt="openmentor.io"
            unoptimized
          />
        </Link>

        <div className="rounded-[20px] border border-line bg-white px-6 py-8 shadow-[0_20px_50px_-24px_rgba(19,42,82,0.22)] sm:px-10">
          <div className="text-center">
            {state === 'verifying' && (
              <>
                <FontAwesomeIcon
                  icon={faCircleNotch}
                  className="mb-4 animate-spin text-4xl text-brand-cobalt"
                />
                <h2 className="mb-2 text-lg text-ink">Verifying the link...</h2>
                <p className="my-0 text-sm text-ink-soft">
                  Hang on, this will take a couple of seconds
                </p>
              </>
            )}

            {state === 'success' && (
              <>
                <FontAwesomeIcon icon={faCheckCircle} className="mb-4 text-4xl text-brand-mint" />
                <h2 className="mb-2 text-lg text-ink">You&apos;re signed in!</h2>
                <p className="my-0 text-sm text-ink-soft">Redirecting to your dashboard...</p>
              </>
            )}

            {state === 'error' && (
              <>
                <FontAwesomeIcon icon={faTimesCircle} className="mb-4 text-4xl text-danger" />
                <h2 className="mb-2 text-lg text-ink">Sign-in failed</h2>
                <p className="my-0 mb-4 text-sm text-ink-soft">{errorMessage}</p>
                <Link href="/mentor/login" className="button">
                  Try again
                </Link>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default function MentorAuthCallbackPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <CallbackHandler />
    </MentorAuthProvider>
  )
}
