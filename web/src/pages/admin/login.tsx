import { useEffect, useState } from 'react'
import Head from 'next/head'
import Image from 'next/image'
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
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  return (
    <div className="flex min-h-screen flex-col justify-center bg-surface px-5 py-12">
      <Head>
        <title>Moderation — sign in — openmentor.io</title>
      </Head>

      <div className="mx-auto w-full max-w-[440px]">
        <div className="rounded-[20px] border border-line bg-white p-7 shadow-[0_20px_50px_-24px_rgba(19,42,82,0.22)] sm:p-10">
          <div className="mb-5 flex items-center gap-2.5">
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

          <h1 className="text-[26px] tracking-[-0.02em] text-ink">Moderation</h1>
          <p className="my-0 mb-5 mt-2 text-sm leading-normal text-ink-soft">
            Sign in with a one-time link.
          </p>

          {expired === 'true' && (
            <div className="mb-5 rounded-field border border-line bg-surface p-3.5">
              <p className="my-0 text-sm text-ink">
                Your session has expired. Please sign in again.
              </p>
            </div>
          )}

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
            <div className="text-center">
              <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-brand-mint/15">
                <FontAwesomeIcon icon={faEnvelope} className="text-mint-ink" />
              </div>
              <h2 className="mb-2 text-lg text-ink">Check your email</h2>
              <p className="my-0 mb-4 text-sm text-ink-soft">
                If the email is registered as a moderator, a login link is already on its way.
              </p>
              <button
                onClick={() => setSubmitSuccess(false)}
                className="text-[13px] font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
              >
                Send again
              </button>
            </div>
          ) : (
            <form onSubmit={handleSubmit(onSubmit)} className="space-y-5" noValidate>
              <div className="flex flex-col gap-1.5">
                <label htmlFor="email" className="text-[13px] font-semibold text-ink">
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
                  className={errors.email ? 'field field-error animate-shake' : 'field'}
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
