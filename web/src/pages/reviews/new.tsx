/**
 * New Review Page
 *
 * Allows mentees to submit feedback about their mentorship session.
 * URL: /reviews/new?request_id=<uuid>
 */

import { useState, useEffect, useRef } from 'react'
import Head from 'next/head'
import Image from 'next/image'
import { useRouter } from 'next/router'
import { useForm } from 'react-hook-form'
import ReCAPTCHA from 'react-google-recaptcha'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faCircleNotch,
  faCheckCircle,
  faExclamationTriangle,
} from '@fortawesome/free-solid-svg-icons'

interface ReviewFormData {
  mentorReview: string
  platformReview: string
  improvements: string
  recaptchaToken: string
}

interface ReviewCheckResponse {
  canSubmit: boolean
  error?: string
  mentorName?: string
}

export default function NewReviewPage(): JSX.Element {
  const router = useRouter()
  const { request_id } = router.query
  const [isLoading, setIsLoading] = useState(false)
  const [isChecking, setIsChecking] = useState(true)
  const [isSuccess, setIsSuccess] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [mentorName, setMentorName] = useState<string>('')
  const recaptchaRef = useRef<ReCAPTCHA>(null)

  const {
    register,
    handleSubmit,
    setValue,
    formState: { errors },
  } = useForm<ReviewFormData>()

  // Check if review can be submitted
  useEffect(() => {
    if (!router.isReady) return

    const requestId = request_id as string
    if (!requestId) {
      setIsChecking(false)
      setCheckError('Invalid link — the request ID is missing')
      return
    }

    const checkReview = async (): Promise<void> => {
      try {
        const response = await fetch(
          `/api/reviews/check?request_id=${encodeURIComponent(requestId)}`
        )
        const data = (await response.json()) as ReviewCheckResponse

        if (!response.ok || !data.canSubmit) {
          setCheckError(data.error || 'A review cannot be submitted for this request')
          if (data.mentorName) {
            setMentorName(data.mentorName)
          }
        } else {
          setMentorName(data.mentorName || '')
        }
      } catch {
        setCheckError('We could not verify the request. Please try again later.')
      } finally {
        setIsChecking(false)
      }
    }

    checkReview()
  }, [router.isReady, request_id])

  const handleCaptchaOnChange = (token: string | null): void => {
    setValue('recaptchaToken', token || '')
  }

  const onSubmit = async (data: ReviewFormData): Promise<void> => {
    if (!request_id) return

    setIsLoading(true)
    setSubmitError(null)

    try {
      const response = await fetch('/api/reviews/submit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          requestId: request_id,
          mentorReview: data.mentorReview,
          platformReview: data.platformReview || '',
          improvements: data.improvements || '',
          recaptchaToken: data.recaptchaToken,
        }),
      })

      if (!response.ok) {
        const errorData = await response.json()
        throw new Error(errorData.error || 'Failed to submit the review')
      }

      setIsSuccess(true)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Something went wrong')
      recaptchaRef.current?.reset()
      setValue('recaptchaToken', '')
    } finally {
      setIsLoading(false)
    }
  }

  const requiredText = 'This field is required.'

  return (
    <>
      <Head>
        <title>Leave a review — openmentor.io</title>
        <meta name="robots" content="noindex,nofollow" />
      </Head>

      <div className="min-h-screen bg-gray-50 flex items-center justify-center px-4 py-12">
        <div className="max-w-2xl w-full">
          {/* Logo */}
          <div className="text-center mb-8">
            <a href="https://openmentor.io" className="inline-block">
              <Image
                src="/brand/logo/svg/logo-horizontal.svg"
                width={165}
                height={45}
                alt="openmentor.io"
                unoptimized
              />
            </a>
          </div>

          {/* Loading State */}
          {isChecking && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8 text-center">
              <FontAwesomeIcon
                icon={faCircleNotch}
                className="animate-spin text-gray-400 text-3xl mb-4"
              />
              <p className="text-gray-600">Loading...</p>
            </div>
          )}

          {/* Error State */}
          {!isChecking && checkError && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8 text-center">
              <div className="mb-4">
                <FontAwesomeIcon
                  icon={faExclamationTriangle}
                  className="text-yellow-500 text-5xl"
                />
              </div>
              <h2 className="text-2xl font-semibold text-gray-900 mb-3">
                We could not open the review form
              </h2>
              <p className="text-gray-600 mb-6">{checkError}</p>
              <a
                href="https://openmentor.io"
                className="inline-block px-6 py-3 bg-brand-navy text-white font-medium rounded-md hover:bg-brand-navy/90 transition-colors"
              >
                Back to home
              </a>
            </div>
          )}

          {/* Success State */}
          {!isChecking && !checkError && isSuccess && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8 text-center">
              <div className="mb-4">
                <FontAwesomeIcon icon={faCheckCircle} className="text-brand-mint text-5xl" />
              </div>
              <h2 className="text-2xl font-semibold text-gray-900 mb-3">
                Thank you for your review!
              </h2>
              <p className="text-gray-600 mb-6">
                Your feedback means a lot to us. It helps mentors get better and lets us improve the
                service.
              </p>
              <a
                href="https://openmentor.io"
                className="inline-block px-6 py-3 bg-brand-navy text-white font-medium rounded-md hover:bg-brand-navy/90 transition-colors"
              >
                Back to home
              </a>
            </div>
          )}

          {/* Form State */}
          {!isChecking && !checkError && !isSuccess && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
              <h2 className="text-2xl font-semibold text-gray-900 mb-2">
                {mentorName
                  ? `Leave a review about your session with ${mentorName}`
                  : 'How was your session with the mentor?'}
              </h2>
              <p className="text-gray-600 mb-6">
                We&apos;re glad you used our service. Please tell us how the session went — your
                feedback means a lot to us and to the mentor.
              </p>

              <form className="space-y-6" onSubmit={handleSubmit(onSubmit)}>
                {/* Mentor Review (required) */}
                <div>
                  <label
                    htmlFor="mentorReview"
                    className="block text-sm font-medium text-gray-700 mb-2"
                  >
                    Review of the mentor <span className="text-red-500">*</span>
                  </label>

                  {errors.mentorReview?.type === 'required' && (
                    <div className="text-sm text-red-700 mb-2">{requiredText}</div>
                  )}
                  {errors.mentorReview?.type === 'minLength' && (
                    <div className="text-sm text-red-700 mb-2">
                      The review must be at least 10 characters long.
                    </div>
                  )}
                  {errors.mentorReview?.type === 'maxLength' && (
                    <div className="text-sm text-red-700 mb-2">
                      Character limit exceeded (5000 max).
                    </div>
                  )}

                  <textarea
                    {...register('mentorReview', {
                      required: true,
                      minLength: 10,
                      maxLength: 5000,
                    })}
                    id="mentorReview"
                    rows={6}
                    placeholder="Tell us how the session went: what you liked, what was useful, whether the mentor helped..."
                    className="w-full px-4 py-3 border border-gray-300 rounded-md focus:ring-2 focus:ring-brand-cobalt focus:border-brand-cobalt resize-vertical"
                    disabled={isLoading}
                  />
                </div>

                {/* Platform Review (optional) */}
                <div>
                  <label
                    htmlFor="platformReview"
                    className="block text-sm font-medium text-gray-700 mb-2"
                  >
                    Review of the platform
                  </label>

                  {errors.platformReview?.type === 'maxLength' && (
                    <div className="text-sm text-red-700 mb-2">
                      Character limit exceeded (5000 max).
                    </div>
                  )}

                  <textarea
                    {...register('platformReview', { maxLength: 5000 })}
                    id="platformReview"
                    rows={3}
                    placeholder="What do you think of the OpenMentor service? (optional)"
                    className="w-full px-4 py-3 border border-gray-300 rounded-md focus:ring-2 focus:ring-brand-cobalt focus:border-brand-cobalt resize-vertical"
                    disabled={isLoading}
                  />
                </div>

                {/* Improvements (optional) */}
                <div>
                  <label
                    htmlFor="improvements"
                    className="block text-sm font-medium text-gray-700 mb-2"
                  >
                    What could we improve?
                  </label>

                  {errors.improvements?.type === 'maxLength' && (
                    <div className="text-sm text-red-700 mb-2">
                      Character limit exceeded (5000 max).
                    </div>
                  )}

                  <textarea
                    {...register('improvements', { maxLength: 5000 })}
                    id="improvements"
                    rows={3}
                    placeholder="Your suggestions for improvement (optional)"
                    className="w-full px-4 py-3 border border-gray-300 rounded-md focus:ring-2 focus:ring-brand-cobalt focus:border-brand-cobalt resize-vertical"
                    disabled={isLoading}
                  />
                </div>

                {/* ReCAPTCHA */}
                <input type="hidden" {...register('recaptchaToken', { required: true })} />

                {errors.recaptchaToken?.type === 'required' && (
                  <div className="text-sm text-red-700">Please confirm you are not a robot.</div>
                )}

                <ReCAPTCHA
                  ref={recaptchaRef}
                  sitekey={process.env.NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY || ''}
                  onChange={handleCaptchaOnChange}
                  hl="en"
                />

                {/* Submit Error */}
                {submitError && (
                  <div className="p-4 rounded-md bg-red-50 border border-red-200">
                    <p className="text-sm text-red-600">{submitError}</p>
                  </div>
                )}

                <button
                  type="submit"
                  disabled={isLoading}
                  className="w-full px-6 py-3 bg-brand-navy text-white font-medium rounded-md hover:bg-brand-navy/90 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-brand-cobalt disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                >
                  {isLoading ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="animate-spin mr-2" />
                      Submitting...
                    </>
                  ) : (
                    'Submit review'
                  )}
                </button>
              </form>

              <div className="mt-6 pt-6 border-t border-gray-200">
                <p className="text-sm text-gray-500 text-center">
                  If you run into any issues, write to us:{' '}
                  <a
                    href="mailto:hello@openmentor.io"
                    className="text-brand-cobalt hover:text-brand-cobalt/80"
                  >
                    hello@openmentor.io
                  </a>
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
