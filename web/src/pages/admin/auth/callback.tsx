import { useEffect, useState } from 'react'
import Head from 'next/head'
import Image from 'next/image'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faCheckCircle, faTimesCircle } from '@fortawesome/free-solid-svg-icons'
import { AdminAuthProvider, useAdminAuth } from '@/components/admin-moderation'
import analytics from '@/lib/analytics'

type CallbackState = 'verifying' | 'success' | 'error'

function CallbackHandler(): JSX.Element {
  const router = useRouter()
  const { verifyLogin, isAuthenticated } = useAdminAuth()
  const [state, setState] = useState<CallbackState>('verifying')
  const [errorMessage, setErrorMessage] = useState<string>('')

  useEffect(() => {
    const token = router.query.token as string | undefined

    if (isAuthenticated) {
      router.replace('/admin/mentors/pending')
      return
    }

    if (!router.isReady) return

    if (!token) {
      analytics.event(analytics.events.ADMIN_AUTH_LOGIN_VERIFIED, {
        outcome: 'invalid_token',
      })
      router.replace('/admin/login?callback_error=invalid_token')
      return
    }

    const verify = async (): Promise<void> => {
      try {
        const result = await verifyLogin(token)
        if (result.success) {
          setState('success')
          analytics.event(analytics.events.ADMIN_AUTH_LOGIN_VERIFIED, {
            outcome: 'success',
          })
          setTimeout(() => {
            router.replace('/admin/mentors/pending')
          }, 1500)
        } else {
          setState('error')
          analytics.event(analytics.events.ADMIN_AUTH_LOGIN_VERIFIED, {
            outcome: 'error',
            error_type: 'invalid_token',
          })
          setErrorMessage(result.message || 'The link is invalid or has expired')
        }
      } catch {
        setState('error')
        analytics.event(analytics.events.ADMIN_AUTH_LOGIN_VERIFIED, {
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
        <title>Moderation sign-in — openmentor.io</title>
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
                <p className="my-0 text-sm text-ink-soft">
                  Redirecting to the moderation panel...
                </p>
              </>
            )}

            {state === 'error' && (
              <>
                <FontAwesomeIcon icon={faTimesCircle} className="mb-4 text-4xl text-danger" />
                <h2 className="mb-2 text-lg text-ink">Sign-in failed</h2>
                <p className="my-0 mb-4 text-sm text-ink-soft">{errorMessage}</p>
                <Link href="/admin/login" className="button">
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

export default function AdminAuthCallbackPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <CallbackHandler />
    </AdminAuthProvider>
  )
}
