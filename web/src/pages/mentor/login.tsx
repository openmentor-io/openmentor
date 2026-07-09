/**
 * Mentor Login Page
 *
 * Passwordless authentication using email + magic link/token.
 */

import { useState, useEffect } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
import { useForm } from 'react-hook-form'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faEnvelope } from '@fortawesome/free-solid-svg-icons'
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
      <div className="flex items-center justify-center min-h-screen">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-brand-cobalt text-2xl" />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50 flex flex-col justify-center py-12 sm:px-6 lg:px-8">
      <Head>
        <title>Mentor login — openmentor.io</title>
      </Head>

      <div className="sm:mx-auto sm:w-full sm:max-w-md">
        <Link href="/" className="flex justify-center mb-6">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={165}
            height={45}
            alt="openmentor.io"
            unoptimized
          />
        </Link>
        <h2 className="text-center text-2xl font-semibold text-gray-900">Mentor dashboard</h2>
        <p className="mt-2 text-center text-sm text-gray-600">Sign in to manage your requests</p>
      </div>

      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-white py-8 px-4 shadow-lg sm:rounded-lg sm:px-10">
          {/* Session expired message */}
          {expired === 'true' && (
            <div className="mb-6 p-4 rounded-md bg-yellow-50 border border-yellow-200">
              <p className="text-sm text-yellow-800">
                Your session has expired. Please sign in again.
              </p>
            </div>
          )}

          {/* Callback error message */}
          {callback_error && (
            <div className="mb-6 p-4 rounded-md bg-red-50 border border-red-200">
              <p className="text-sm text-red-800">
                {callback_error === 'invalid_token'
                  ? 'The link is invalid or has expired. Please request a new one.'
                  : 'Something went wrong. Please try again.'}
              </p>
            </div>
          )}

          {submitSuccess ? (
            /* Success state */
            <div className="text-center">
              <div className="mx-auto flex items-center justify-center h-12 w-12 rounded-full bg-green-100 mb-4">
                <FontAwesomeIcon icon={faEnvelope} className="text-green-600" />
              </div>
              <h3 className="text-lg font-medium text-gray-900 mb-2">Check your email</h3>
              <p className="text-sm text-gray-600 mb-4">
                We&apos;ve sent a login link to the email you provided. Follow it to sign in to your
                dashboard.
              </p>
              <p className="text-xs text-gray-500">
                Didn&apos;t get the email? Check your spam folder or{' '}
                <button
                  onClick={() => setSubmitSuccess(false)}
                  className="text-brand-cobalt hover:text-brand-cobalt/80"
                >
                  try again
                </button>
              </p>
            </div>
          ) : (
            /* Login form */
            <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
              <div>
                <label htmlFor="email" className="block text-sm font-medium text-gray-700">
                  Email
                </label>
                <div className="mt-1">
                  <input
                    id="email"
                    type="email"
                    autoComplete="email"
                    placeholder="mentor@example.com"
                    {...register('email', {
                      required: 'Enter your email',
                      pattern: {
                        value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
                        message: 'Enter a valid email',
                      },
                    })}
                    className="appearance-none block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm placeholder-gray-400 focus:outline-none focus:ring-brand-cobalt focus:border-brand-cobalt sm:text-sm"
                  />
                </div>
                {errors.email && (
                  <p className="mt-2 text-sm text-red-600">{errors.email.message}</p>
                )}
              </div>

              {submitError && (
                <div className="p-3 rounded-md bg-red-50 border border-red-200">
                  <p className="text-sm text-red-600">{submitError}</p>
                </div>
              )}

              <button
                type="submit"
                disabled={isSubmitting}
                className="w-full flex justify-center py-2 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-brand-navy hover:bg-brand-navy/90 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-brand-cobalt disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {isSubmitting ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="animate-spin mr-2" />
                    Sending...
                  </>
                ) : (
                  'Get a login link'
                )}
              </button>
            </form>
          )}

          {/* Help text */}
          <div className="mt-6 border-t border-gray-200 pt-6">
            <p className="text-xs text-gray-500 text-center">
              Use the email you provided when registering as a mentor. If you run into any issues,{' '}
              <a
                href="mailto:hello@openmentor.io"
                className="text-brand-cobalt hover:text-brand-cobalt/80"
              >
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
