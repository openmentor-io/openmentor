import { useEffect, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import classNames from 'classnames'
import { Turnstile } from '@marsidev/react-turnstile'
import TextareaAutosize from 'react-textarea-autosize'

interface ContactFormData {
  email: string
  name: string
  intro: string
  experience?: string
  contact: string
  captchaToken: string
}

interface ContactMentorFormProps {
  isLoading: boolean
  isError: boolean
  onSubmit: (data: ContactFormData) => void
  /** Used in the fine print next to the submit button (design 03). */
  mentorFirstName?: string
}

const INTRO_MAX_LENGTH = 4000

/** Inline field error: danger dot icon + message, slides in (design 03). */
function FieldError({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="flex animate-dropdown-in items-center gap-1.5 text-xs font-medium text-danger">
      <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true" className="flex-none">
        <circle cx="6" cy="6" r="6" fill="currentColor" />
        <path d="M6 3v3.5M6 8.5v.5" stroke="#fff" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
      {children}
    </span>
  )
}

function FieldLabel({
  htmlFor,
  required,
  children,
}: {
  htmlFor: string
  required?: boolean
  children: ReactNode
}): JSX.Element {
  return (
    <label htmlFor={htmlFor} className="text-[13px] font-semibold text-ink">
      {children}
      {required ? (
        <span className="text-danger"> *</span>
      ) : (
        <span className="text-xs font-normal text-ink-soft"> (optional)</span>
      )}
    </label>
  )
}

export default function ContactMentorForm({
  isLoading,
  isError,
  onSubmit,
  mentorFirstName,
}: ContactMentorFormProps): JSX.Element {
  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors, submitCount },
  } = useForm<ContactFormData>()

  const requiredText = 'This field is required.'
  const introValue = watch('intro') || ''

  // Error shake: re-trigger the ±3px shake on every failed submit attempt
  // (motion spec: shake ±3px ×2, 200ms; message slides down).
  const [isShaking, setIsShaking] = useState(false)
  useEffect(() => {
    if (submitCount === 0 || Object.keys(errors).length === 0) {
      return undefined
    }
    setIsShaking(true)
    const timer = setTimeout(() => setIsShaking(false), 250)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [submitCount]) // Intentionally keyed on submit attempts only

  const fieldClass = (hasError: boolean): string =>
    classNames('field', hasError && 'field-error', hasError && isShaking && 'animate-shake')

  const handleCaptchaOnSuccess = (token: string): void => {
    setValue('captchaToken', token)
  }

  const handleCaptchaOnExpire = (): void => {
    setValue('captchaToken', '')
  }

  return (
    <form className="flex flex-col gap-5" onSubmit={handleSubmit(onSubmit)} noValidate>
      <div className="flex flex-col gap-4 sm:flex-row">
        <div className="flex flex-1 flex-col gap-1.5">
          <FieldLabel htmlFor="name" required>
            Your full name
          </FieldLabel>

          <input
            type="text"
            {...register('name', { required: true })}
            id="name"
            autoComplete="name"
            aria-invalid={Boolean(errors.name)}
            className={fieldClass(Boolean(errors.name))}
          />

          {errors.name?.type === 'required' && <FieldError>{requiredText}</FieldError>}
        </div>

        <div className="flex flex-1 flex-col gap-1.5">
          <FieldLabel htmlFor="email" required>
            Your email
          </FieldLabel>

          <input
            type="email"
            {...register('email', { required: true, pattern: /^\S+@\S+$/i })}
            id="email"
            autoComplete="email"
            aria-invalid={Boolean(errors.email)}
            className={fieldClass(Boolean(errors.email))}
          />

          {errors.email?.type === 'required' && <FieldError>{requiredText}</FieldError>}
          {errors.email?.type === 'pattern' && (
            <FieldError>That doesn&apos;t look like a full email address.</FieldError>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <FieldLabel htmlFor="intro" required>
          What would you like to talk about?
        </FieldLabel>

        <TextareaAutosize
          {...register('intro', { required: true, maxLength: INTRO_MAX_LENGTH, minLength: 10 })}
          id="intro"
          aria-invalid={Boolean(errors.intro)}
          className={classNames(fieldClass(Boolean(errors.intro)), 'leading-relaxed')}
          minRows={4}
        />

        {errors.intro?.type === 'required' && <FieldError>{requiredText}</FieldError>}
        {errors.intro?.type === 'minLength' && (
          <FieldError>The message must be at least 10 characters long.</FieldError>
        )}
        {errors.intro?.type === 'maxLength' && (
          <FieldError>Character limit exceeded ({INTRO_MAX_LENGTH} max).</FieldError>
        )}

        <div className="flex items-baseline justify-between gap-3">
          <span className="text-xs text-ink-soft">
            In a few words, tell the mentor exactly how they can help you.
          </span>
          <span className="meta-mono flex-none text-ink-mute">
            {introValue.length}/{INTRO_MAX_LENGTH}
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <FieldLabel htmlFor="experience">How would you rate your level?</FieldLabel>

        <select {...register('experience')} id="experience" className="field">
          <option></option>
          <option>Junior</option>
          <option>Middle</option>
          <option>Senior</option>
          <option>Manager</option>
          <option>Manager of managers</option>
          <option>C-level</option>
        </select>
      </div>

      <div className="flex flex-col gap-1.5">
        <FieldLabel htmlFor="contact">How can your mentor reach you?</FieldLabel>

        <input
          type="text"
          {...register('contact', { maxLength: 100 })}
          id="contact"
          aria-invalid={Boolean(errors.contact)}
          className={fieldClass(Boolean(errors.contact))}
        />

        {errors.contact?.type === 'maxLength' && (
          <FieldError>The contact details must be 100 characters or fewer.</FieldError>
        )}

        <span className="text-xs text-ink-soft">
          Email, Telegram, LinkedIn, whatever works for you. Your mentor will use your email
          otherwise.
        </span>
      </div>

      <input type="hidden" {...register('captchaToken', { required: true })} id="captchaToken" />

      <div className="flex flex-col gap-1.5">
        <Turnstile
          siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
          onSuccess={handleCaptchaOnSuccess}
          onExpire={handleCaptchaOnExpire}
          options={{ language: 'en' }}
        />

        {errors.captchaToken?.type === 'required' && (
          <FieldError>Please complete the verification above.</FieldError>
        )}
      </div>

      {isError && (
        <div className="flex animate-dropdown-in items-start gap-2.5 rounded-field border border-danger/40 bg-danger/5 px-4 py-3.5 text-sm font-medium text-danger">
          Something went wrong. We&apos;re probably already fixing it — please try again later.
        </div>
      )}

      <div className="mt-1.5 flex flex-col gap-3 sm:flex-row sm:items-center">
        <button className="button px-7 py-[15px] text-[15px]" type="submit" disabled={isLoading}>
          Send request
        </button>
        <span className="text-[13px] leading-normal text-ink-soft">
          No account needed. We only share your email with {mentorFirstName || 'the mentor'}.
        </span>
      </div>
    </form>
  )
}
