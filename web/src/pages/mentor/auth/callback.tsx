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
    <div className="min-h-screen bg-gray-50 flex flex-col justify-center py-12 sm:px-6 lg:px-8">
      <Head>
        <title>Sign in — openmentor.io</title>
      </Head>

      <div className="sm:mx-auto sm:w-full sm:max-w-md">
        <Link href="/" className="flex justify-center mb-8">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={165}
            height={45}
            alt="openmentor.io"
            unoptimized
          />
        </Link>

        <div className="bg-white py-8 px-4 shadow-lg sm:rounded-lg sm:px-10">
          <div className="text-center">
            {state === 'verifying' && (
              <>
                <FontAwesomeIcon
                  icon={faCircleNotch}
                  className="animate-spin text-brand-cobalt text-4xl mb-4"
                />
                <h2 className="text-lg font-medium text-gray-900 mb-2">Verifying the link...</h2>
                <p className="text-sm text-gray-600">Hang on, this will take a couple of seconds</p>
              </>
            )}

            {state === 'success' && (
              <>
                <FontAwesomeIcon icon={faCheckCircle} className="text-brand-mint text-4xl mb-4" />
                <h2 className="text-lg font-medium text-gray-900 mb-2">You&apos;re signed in!</h2>
                <p className="text-sm text-gray-600">Redirecting to your dashboard...</p>
              </>
            )}

            {state === 'error' && (
              <>
                <FontAwesomeIcon icon={faTimesCircle} className="text-red-500 text-4xl mb-4" />
                <h2 className="text-lg font-medium text-gray-900 mb-2">Sign-in failed</h2>
                <p className="text-sm text-gray-600 mb-4">{errorMessage}</p>
                <Link
                  href="/mentor/login"
                  className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md text-white bg-brand-navy hover:bg-brand-navy/90 transition-colors"
                >
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
