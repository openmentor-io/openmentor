/**
 * Mentor Profile Edit Page (design 09)
 *
 * Allows mentors to edit their profile using session-based auth. On top of
 * the form it renders the profile lifecycle state:
 * - draft + moderation note  -> "returned for edits" banner + submit for review
 * - draft without note       -> "confirm your email" banner (confirmation submits)
 * - pending                  -> "in review" banner
 * - active / inactive        -> the visibility toggle card
 */

import { useState, useEffect, Fragment } from 'react'
import Head from 'next/head'
import Link from 'next/link'
import { Transition } from '@headlessui/react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import type {
  MentorWithSecureFields,
  SaveProfileRequest,
  UploadProfilePictureRequest,
} from '@/types'
import { hasMentorSecureFields } from '@/types'
import {
  MentorAuthProvider,
  useMentorAuth,
  MentorAdminLayout,
  ProfileVisibilityCard,
  ShareProfileCard,
} from '@/components/mentor-admin'
import { ProfileForm, Notification } from '@/components'
import { useRouter } from 'next/router'
import { captureException } from '@/lib/posthog'

type ReadyStatus = '' | 'loading' | 'success' | 'error'
type ImageUploadStatus = 'idle' | 'loading' | 'success' | 'error'

const toastTransition = {
  enter: 'transform ease-out duration-300 transition',
  enterFrom: 'translate-y-2 opacity-0 sm:translate-y-0 sm:translate-x-2',
  enterTo: 'translate-y-0 opacity-100 sm:translate-x-0',
  leave: 'transition ease-in duration-100',
  leaveFrom: 'opacity-100',
  leaveTo: 'opacity-0',
}

interface StatusBannerProps {
  accent: 'danger' | 'cobalt' | 'navy'
  label: string
  children: React.ReactNode
}

function StatusBanner({ accent, label, children }: StatusBannerProps): JSX.Element {
  const accentClass = {
    danger: 'border-l-danger',
    cobalt: 'border-l-brand-cobalt',
    navy: 'border-l-brand-navy',
  }[accent]

  return (
    <div
      className={`animate-rise-in rounded-panel border border-l-4 border-line bg-white p-5 sm:px-[26px] sm:py-[22px] ${accentClass}`}
    >
      <div className="font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
        {label}
      </div>
      {children}
    </div>
  )
}

function ProfileEditContent(): JSX.Element {
  const router = useRouter()
  const { isAuthenticated, isLoading: authLoading, session } = useMentorAuth()
  const [mentor, setMentor] = useState<MentorWithSecureFields | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [readyStatus, setReadyStatus] = useState<ReadyStatus>('')
  const [showSuccess, setShowSuccess] = useState(false)
  const [imageUploadStatus, setImageUploadStatus] = useState<ImageUploadStatus>('idle')
  const [uploadedImageUrl, setUploadedImageUrl] = useState<string | null>(null)
  const [tempImagePreview, setTempImagePreview] = useState<string | null>(null)
  const [showVisibilitySuccess, setShowVisibilitySuccess] = useState(false)
  const [isSubmittingForReview, setIsSubmittingForReview] = useState(false)
  const [submitReviewError, setSubmitReviewError] = useState<string | null>(null)
  const [showSubmitSuccess, setShowSubmitSuccess] = useState(false)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/mentor/login')
    }
  }, [authLoading, isAuthenticated, router])

  // Load mentor profile
  useEffect(() => {
    if (!isAuthenticated || !session) return

    const loadMentor = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const response = await fetch('/api/mentor/profile', {
          credentials: 'include',
        })
        if (!response.ok) {
          throw new Error('Failed to load profile')
        }
        const data = await response.json()
        if (data.mentor && hasMentorSecureFields(data.mentor)) {
          setMentor(data.mentor)
        } else {
          setError('Profile not found')
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load profile')
      } finally {
        setIsLoading(false)
      }
    }

    loadMentor()
  }, [isAuthenticated, session])

  // Show success notification
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    if (readyStatus === 'success') {
      setShowSuccess(true)
      timer = setTimeout(() => setShowSuccess(false), 3000)
    }
    return () => {
      if (timer) {
        clearTimeout(timer)
      }
    }
  }, [readyStatus])

  // Show profile visibility success notification
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    if (showVisibilitySuccess) {
      timer = setTimeout(() => setShowVisibilitySuccess(false), 3000)
    }
    return () => {
      if (timer) {
        clearTimeout(timer)
      }
    }
  }, [showVisibilitySuccess])

  // Show submitted-for-review success notification
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    if (showSubmitSuccess) {
      timer = setTimeout(() => setShowSubmitSuccess(false), 5000)
    }
    return () => {
      if (timer) {
        clearTimeout(timer)
      }
    }
  }, [showSubmitSuccess])

  const onVisibilityChange = (status: 'active' | 'inactive'): void => {
    setMentor((current) => (current ? { ...current, status } : current))
    setShowVisibilitySuccess(true)
  }

  const onSubmitForReview = async (): Promise<void> => {
    if (isSubmittingForReview) return

    setIsSubmittingForReview(true)
    setSubmitReviewError(null)

    try {
      const response = await fetch('/api/mentor/profile/submit', {
        method: 'POST',
        credentials: 'include',
      })

      const data = await response.json().catch(() => null)

      if (!response.ok || !data?.success) {
        throw new Error(data?.error || 'Failed to submit the profile')
      }

      // Status refreshes to pending — the banner becomes the pending one
      setMentor((current) => (current ? { ...current, status: 'pending' } : current))
      setShowSubmitSuccess(true)
    } catch (e) {
      setSubmitReviewError('Failed to submit your profile for review. Please try again.')
      if (e instanceof Error) {
        captureException(e, { page: 'edit-profile', action: 'submit-for-review' })
      }
      console.error('Profile submit-for-review error:', e)
    } finally {
      setIsSubmittingForReview(false)
    }
  }

  const onSubmit = async (data: SaveProfileRequest): Promise<void> => {
    if (readyStatus === 'loading' || !mentor) return

    setReadyStatus('loading')

    try {
      const response = await fetch('/api/mentor/profile', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
      })

      if (!response.ok) {
        throw new Error('Failed to save profile')
      }

      const result = await response.json()
      setReadyStatus(result.success ? 'success' : 'error')
    } catch (e) {
      setReadyStatus('error')
      if (e instanceof Error) {
        captureException(e, { page: 'edit-profile', action: 'save' })
      }
      console.error('Profile save error:', e)
    }
  }

  const onImageUpload = async (
    imageData: UploadProfilePictureRequest,
    onSuccess?: () => void
  ): Promise<void> => {
    if (imageUploadStatus === 'loading' || !mentor) return

    setImageUploadStatus('loading')
    setTempImagePreview(imageData.image)

    try {
      const response = await fetch('/api/mentor/profile/picture', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(imageData),
      })

      if (!response.ok) {
        throw new Error('Failed to upload image')
      }

      const data = await response.json()
      if (data.success) {
        setImageUploadStatus('success')
        if (data.imageUrl) {
          setUploadedImageUrl(data.imageUrl)
        }
        if (onSuccess) {
          onSuccess()
        }
        setTimeout(() => setImageUploadStatus('idle'), 5000)
      } else {
        setImageUploadStatus('error')
        setTempImagePreview(null)
      }
    } catch (e) {
      setImageUploadStatus('error')
      setTempImagePreview(null)
      if (e instanceof Error) {
        captureException(e, { page: 'edit-profile', action: 'image-upload' })
      }
      console.error('Profile picture upload error:', e)
    }
  }

  // Show loading while checking auth
  if (authLoading || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    )
  }

  const isReturned = mentor?.status === 'draft' && !!mentor?.moderationNote
  const isAwaitingConfirm = mentor?.status === 'draft' && !mentor?.moderationNote
  const isPending = mentor?.status === 'pending'
  const isApproved = mentor?.status === 'active' || mentor?.status === 'inactive'

  return (
    <>
      <Head>
        <title>My profile — openmentor.io</title>
      </Head>

      <MentorAdminLayout
        title="My profile"
        actions={
          !isLoading && mentor && isApproved ? (
            <Link
              href={'/mentor/' + mentor.slug}
              target="_blank"
              className="button-secondary text-[13px]"
            >
              Preview public page ↗
            </Link>
          ) : undefined
        }
      >
        {/* Loading state */}
        {isLoading && (
          <div className="flex flex-col items-center justify-center py-12">
            <FontAwesomeIcon
              icon={faCircleNotch}
              className="mb-3 animate-spin text-2xl text-brand-cobalt"
            />
            <p className="my-0 text-sm text-ink-soft">Loading profile...</p>
          </div>
        )}

        {/* Error state */}
        {error && !isLoading && (
          <div className="rounded-panel border border-danger/40 bg-white p-6 text-center">
            <p className="my-0 mb-4 text-sm font-medium text-danger">{error}</p>
            <button
              onClick={() => window.location.reload()}
              className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
            >
              Try again
            </button>
          </div>
        )}

        {/* Workflow banners + visibility + profile form */}
        {!isLoading && !error && mentor && (
          <div className="space-y-4">
            {/* Returned for edits (draft with a reviewer note) */}
            {isReturned && (
              <StatusBanner accent="danger" label="Returned for edits">
                <p className="my-0 mt-2 whitespace-pre-wrap text-sm leading-relaxed text-ink">
                  {mentor.moderationNote}
                </p>
                <p className="my-0 mt-2 text-[13px] text-ink-soft">
                  Edit and resubmit — most fixes get approved within a day.
                </p>
                {submitReviewError && (
                  <p className="my-0 mt-3 text-sm font-medium text-danger" role="alert">
                    {submitReviewError}
                  </p>
                )}
                <button
                  onClick={onSubmitForReview}
                  disabled={isSubmittingForReview}
                  className="button mt-4"
                >
                  {isSubmittingForReview ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                      Submitting...
                    </>
                  ) : (
                    'Submit for review'
                  )}
                </button>
              </StatusBanner>
            )}

            {/* Draft awaiting email confirmation (no note) */}
            {isAwaitingConfirm && (
              <StatusBanner accent="cobalt" label="Confirm your email">
                <p className="my-0 mt-2 text-sm leading-relaxed text-ink-soft">
                  Your profile isn&apos;t in review yet. Check your inbox and follow the
                  confirmation link — that submits your profile to our moderators. You can keep
                  editing in the meantime.
                </p>
              </StatusBanner>
            )}

            {/* Pending review */}
            {isPending && (
              <StatusBanner accent="navy" label="In review">
                <p className="my-0 mt-2 text-sm leading-relaxed text-ink-soft">
                  Your profile is in review — we&apos;ll email you as soon as it&apos;s approved.
                  You can keep editing in the meantime.
                </p>
              </StatusBanner>
            )}

            {/* Visibility toggle — only for approved (active/inactive) profiles */}
            {(mentor.status === 'active' || mentor.status === 'inactive') && (
              <ProfileVisibilityCard initialStatus={mentor.status} onSuccess={onVisibilityChange} />
            )}

            {/* Share card — only while the profile is live (a hidden profile
                has no contact button, so sharing it would disappoint). */}
            {mentor.status === 'active' && mentor.slug && <ShareProfileCard slug={mentor.slug} />}

            <div className="rounded-panel border border-line bg-white p-5 sm:p-7">
              <ProfileForm
                mentor={{
                  ...mentor,
                  photo_url: uploadedImageUrl || mentor.photo_url,
                }}
                isLoading={readyStatus === 'loading'}
                isError={readyStatus === 'error'}
                onSubmit={onSubmit}
                onImageUpload={onImageUpload}
                imageUploadStatus={imageUploadStatus}
                tempImagePreview={tempImagePreview}
              />
            </div>
          </div>
        )}

        {/* Toast notifications */}
        <div
          aria-live="assertive"
          className="pointer-events-none fixed inset-0 z-10 flex items-end px-4 py-6 sm:p-6"
        >
          <div className="flex w-full flex-col items-center space-y-4 sm:items-end">
            <Transition show={showSuccess} as={Fragment} {...toastTransition}>
              <Notification
                content="Changes saved successfully"
                onClose={() => setShowSuccess(false)}
              />
            </Transition>
            <Transition show={showVisibilitySuccess} as={Fragment} {...toastTransition}>
              <Notification
                content="Profile visibility updated"
                onClose={() => setShowVisibilitySuccess(false)}
              />
            </Transition>
            <Transition show={showSubmitSuccess} as={Fragment} {...toastTransition}>
              <Notification
                content={
                  <>
                    Profile submitted for review. <b>We&apos;ll email you.</b>
                  </>
                }
                onClose={() => setShowSubmitSuccess(false)}
              />
            </Transition>
          </div>
        </div>
      </MentorAdminLayout>
    </>
  )
}

export default function MentorProfileEditPage(): JSX.Element {
  return (
    <MentorAuthProvider>
      <ProfileEditContent />
    </MentorAuthProvider>
  )
}
