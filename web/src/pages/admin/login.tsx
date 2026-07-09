import { useEffect, useState } from 'react'
import Head from 'next/head'
import Image from 'next/image'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { useForm } from 'react-hook-form'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faEnvelope } from '@fortawesome/free-solid-svg-icons'
import { AdminAuthProvider, useAdminAuth } from '@/components/admin-moderation'
import analytics from '@/lib/analytics'

interface LoginFormData {
  email: string
}

function LoginForm(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading, requestLogin } = useAdminAuth()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitSuccess, setSubmitSuccess] = useState(false)
  const { expired, callback_error } = router.query

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginFormData>()

  useEffect(() => {
    if (!authLoading && isAuthenticated) {
      router.replace('/admin/mentors/pending')
    }
  }, [authLoading, isAuthenticated, router])

  useEffect(() => {
    analytics.event(analytics.events.ADMIN_AUTH_LOGIN_REQUESTED, {
      outcome: 'login_page_viewed',
    })
  }, [])

  const onSubmit = async (data: LoginFormData): Promise<void> => {
    setIsSubmitting(true)
    setSubmitError(null)
    analytics.event(analytics.events.ADMIN_AUTH_LOGIN_REQUESTED, {
      outcome: 'submitted',
    })

    try {
      const result = await requestLogin(data.email)
      if (result.success) {
        setSubmitSuccess(true)
        analytics.event(analytics.events.ADMIN_AUTH_LOGIN_REQUESTED, {
          outcome: 'success',
        })
      } else {
        setSubmitError(result.message || 'Something went wrong. Please try again.')
        analytics.event(analytics.events.ADMIN_AUTH_LOGIN_REQUESTED, {
          outcome: 'error',
          error_type: 'api_error',
        })
      }
    } catch {
      setSubmitError('Something went wrong. Please try again.')
      analytics.event(analytics.events.ADMIN_AUTH_LOGIN_REQUESTED, {
        outcome: 'error',
        error_type: 'network_error',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  if (authLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <div className="flex min-h-screen flex-col justify-center bg-gray-50 py-12 sm:px-6 lg:px-8">
      <Head>
        <title>Moderation — sign in — openmentor.io</title>
      </Head>

      <div className="sm:mx-auto sm:w-full sm:max-w-md">
        <Link href="/" className="mb-6 flex justify-center">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={165}
            height={45}
            alt="openmentor.io"
            unoptimized
          />
        </Link>
        <h2 className="text-center text-2xl font-semibold text-gray-900">Moderation panel</h2>
        <p className="mt-2 text-center text-sm text-gray-600">Sign in with a one-time link</p>
      </div>

      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-white px-4 py-8 shadow-lg sm:rounded-lg sm:px-10">
          {expired === 'true' && (
            <div className="mb-6 rounded-md border border-yellow-200 bg-yellow-50 p-4">
              <p className="text-sm text-yellow-800">
                Your session has expired. Please sign in again.
              </p>
            </div>
          )}

          {callback_error && (
            <div className="mb-6 rounded-md border border-red-200 bg-red-50 p-4">
              <p className="text-sm text-red-800">
                {callback_error === 'invalid_token'
                  ? 'The link is invalid or has expired. Please request a new one.'
                  : 'Something went wrong. Please try again.'}
              </p>
            </div>
          )}

          {submitSuccess ? (
            <div className="text-center">
              <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-green-100">
                <FontAwesomeIcon icon={faEnvelope} className="text-green-600" />
              </div>
              <h3 className="mb-2 text-lg font-medium text-gray-900">Check your email</h3>
              <p className="mb-4 text-sm text-gray-600">
                If the email is registered as a moderator, a login link is already on its way.
              </p>
              <button
                onClick={() => setSubmitSuccess(false)}
                className="text-xs text-brand-cobalt hover:text-brand-cobalt/80"
              >
                Send again
              </button>
            </div>
          ) : (
            <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
              <div>
                <label htmlFor="email" className="block text-sm font-medium text-gray-700">
                  Email
                </label>
                <input
                  id="email"
                  type="email"
                  autoComplete="email"
                  placeholder="moderator@openmentor.io"
                  {...register('email', {
                    required: 'Enter your email',
                    pattern: {
                      value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
                      message: 'Enter a valid email',
                    },
                  })}
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-brand-cobalt focus:outline-none"
                />
                {errors.email && (
                  <p className="mt-2 text-sm text-red-600">{errors.email.message}</p>
                )}
              </div>

              {submitError && (
                <div className="rounded-md border border-red-200 bg-red-50 p-3">
                  <p className="text-sm text-red-600">{submitError}</p>
                </div>
              )}

              <button
                type="submit"
                disabled={isSubmitting}
                className="w-full rounded-md bg-brand-navy px-4 py-2 text-sm font-medium text-white hover:bg-brand-navy/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isSubmitting ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                    Sending...
                  </>
                ) : (
                  'Get a login link'
                )}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}

export default function AdminLoginPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <LoginForm />
    </AdminAuthProvider>
  )
}
