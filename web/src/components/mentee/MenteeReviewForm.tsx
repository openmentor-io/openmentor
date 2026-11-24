/**
 * Review form on a completed request in the mentee dashboard (D95). Same fields
 * and limits as /reviews/new; the session authorizes it instead of a token.
 */

import { useId, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import classNames from 'classnames'
import { Turnstile, type TurnstileInstance } from '@marsidev/react-turnstile'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import type { MenteeReview } from '@/types'
import { submitMenteeReview } from '@/lib/mentee-api'
import analytics from '@/lib/analytics'

interface ReviewFormData {
  mentorReview: string
  platformReview: string
  improvements: string
  captchaToken: string
}

interface MenteeReviewFormProps {
  requestId: string
  mentorName: string
  onSubmitted: (review: MenteeReview) => void
  onCancel: () => void
}

export default function MenteeReviewForm({
  requestId,
  mentorName,
  onSubmitted,
  onCancel,
}: MenteeReviewFormProps): JSX.Element {
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const turnstileRef = useRef<TurnstileInstance>(null)
  // Not the request id: it is a legacy review capability, and session replay
  // records element ids verbatim.
  const fieldId = useId()
  const {
    register,
    handleSubmit,
    setValue,
    formState: { errors },
  } = useForm<ReviewFormData>()

  const onSubmit = async (data: ReviewFormData): Promise<void> => {
    setIsSubmitting(true)
    setSubmitError(null)
    try {
      const result = await submitMenteeReview({
        requestId,
        mentorReview: data.mentorReview,
        platformReview: data.platformReview || '',
        improvements: data.improvements || '',
        captchaToken: data.captchaToken,
      })
      if (!result.success) {
        throw new Error(result.error || 'Failed to submit the review')
      }
      analytics.event(analytics.events.MENTEE_REVIEW_SUBMITTED, { outcome: 'success' })
      onSubmitted({
        mentorReview: data.mentorReview.trim(),
        platformReview: (data.platformReview || '').trim(),
        improvements: (data.improvements || '').trim(),
        createdAt: new Date().toISOString(),
      })
    } catch (err) {
      analytics.event(analytics.events.MENTEE_REVIEW_SUBMITTED, { outcome: 'error' })
      setSubmitError(err instanceof Error ? err.message : 'Something went wrong')
      turnstileRef.current?.reset()
      setValue('captchaToken', '')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <form
      onSubmit={handleSubmit(onSubmit)}
      className="mt-4 flex flex-col gap-4 border-t border-line pt-4"
      noValidate
    >
      <p className="my-0 text-sm text-ink-soft">
        How was your session{mentorName ? ` with ${mentorName}` : ''}? Your review is shared with
        the mentor and can&apos;t be edited later.
      </p>

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`mentorReview-${fieldId}`} className="text-[13px] font-semibold text-ink">
          Review of the mentor <span className="text-danger">*</span>
        </label>
        <textarea
          id={`mentorReview-${fieldId}`}
          rows={5}
          disabled={isSubmitting}
          placeholder="What you liked, what was useful, whether the mentor helped..."
          {...register('mentorReview', {
            required: 'This field is required.',
            minLength: { value: 10, message: 'The review must be at least 10 characters long.' },
            maxLength: { value: 5000, message: 'Character limit exceeded (5000 max).' },
          })}
          className={classNames('field resize-y', errors.mentorReview && 'field-error')}
        />
        {errors.mentorReview && (
          <p className="my-0 text-xs font-medium text-danger" role="alert">
            {errors.mentorReview.message}
          </p>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`platformReview-${fieldId}`} className="text-[13px] font-semibold text-ink">
          Review of the platform
        </label>
        <textarea
          id={`platformReview-${fieldId}`}
          rows={2}
          disabled={isSubmitting}
          placeholder="What do you think of OpenMentor? (optional)"
          {...register('platformReview', { maxLength: 5000 })}
          className="field resize-y"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`improvements-${fieldId}`} className="text-[13px] font-semibold text-ink">
          What could we improve?
        </label>
        <textarea
          id={`improvements-${fieldId}`}
          rows={2}
          disabled={isSubmitting}
          placeholder="Your suggestions (optional)"
          {...register('improvements', { maxLength: 5000 })}
          className="field resize-y"
        />
      </div>

      <input type="hidden" {...register('captchaToken', { required: true })} />
      <Turnstile
        ref={turnstileRef}
        siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
        onSuccess={(token) => setValue('captchaToken', token)}
        onExpire={() => setValue('captchaToken', '')}
        options={{ language: 'en' }}
      />
      {errors.captchaToken && (
        <p className="my-0 text-xs font-medium text-danger" role="alert">
          Please confirm you are not a robot.
        </p>
      )}

      {submitError && (
        <p className="my-0 text-sm font-medium text-danger" role="alert">
          {submitError}
        </p>
      )}

      <div className="flex flex-wrap gap-3">
        <button type="submit" disabled={isSubmitting} className="button">
          {isSubmitting ? (
            <>
              <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
              Submitting...
            </>
          ) : (
            'Submit review'
          )}
        </button>
        <button type="button" onClick={onCancel} disabled={isSubmitting} className="button-ghost">
          Cancel
        </button>
      </div>
    </form>
  )
}
