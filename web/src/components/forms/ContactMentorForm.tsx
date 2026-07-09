import { useForm } from 'react-hook-form'
import { Turnstile } from '@marsidev/react-turnstile'
import TextareaAutosize from 'react-textarea-autosize'

interface ContactFormData {
  email: string
  name: string
  intro: string
  experience?: string
  telegramUsername: string
  captchaToken: string
}

interface ContactMentorFormProps {
  isLoading: boolean
  isError: boolean
  onSubmit: (data: ContactFormData) => void
}

export default function ContactMentorForm({
  isLoading,
  isError,
  onSubmit,
}: ContactMentorFormProps): JSX.Element {
  const {
    register,
    handleSubmit,
    setValue,
    formState: { errors },
  } = useForm<ContactFormData>()

  const requiredText = 'This field is required.'

  const handleCaptchaOnSuccess = (token: string): void => {
    setValue('captchaToken', token)
  }

  const handleCaptchaOnExpire = (): void => {
    setValue('captchaToken', '')
  }

  return (
    <form className="space-y-8" onSubmit={handleSubmit(onSubmit)}>
      <div>
        <label htmlFor="email" className="block mb-2 font-medium text-ink">
          Your email
          <span className="text-sm text-red-700 mt-3 mb-2"> *</span>
        </label>

        {errors.email && errors.email.type === 'required' && (
          <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
        )}

        {errors.email && errors.email.type === 'pattern' && (
          <div className="text-sm text-red-700 mt-3 mb-2">Please enter a valid email address.</div>
        )}

        <input
          type="email"
          {...register('email', { required: true, pattern: /^\S+@\S+$/i })}
          id="email"
          autoComplete="email"
          className="field"
        />
      </div>

      <div>
        <label htmlFor="name" className="block mb-2 font-medium text-ink">
          Your full name
          <span className="text-sm text-red-700 mt-3 mb-2"> *</span>
        </label>

        {errors.name && errors.name.type === 'required' && (
          <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
        )}

        <input
          type="text"
          {...register('name', { required: true })}
          id="name"
          autoComplete="name"
          className="field"
        />
      </div>

      <div>
        <label htmlFor="intro" className="block mb-2 font-medium text-ink">
          What would you like to talk about?
          <span className="text-sm text-red-700 mt-3 mb-2"> *</span>
        </label>

        {errors.intro && errors.intro.type === 'maxLength' && (
          <div className="text-sm text-red-700 mt-3 mb-2">Character limit exceeded (4000 max).</div>
        )}

        {errors.intro && errors.intro.type === 'minLength' && (
          <div className="text-sm text-red-700 mt-3 mb-2">
            The message must be at least 10 characters long.
          </div>
        )}

        {errors.intro && errors.intro.type === 'required' && (
          <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
        )}

        <div className="mt-1">
          <TextareaAutosize
            {...register('intro', { required: true, maxLength: 4000, minLength: 10 })}
            id="intro"
            className="field"
            minRows={3}
          />
        </div>

        <p className="mt-2 text-sm text-gray-500">
          In a few words, tell the mentor exactly how they can help you.
        </p>
      </div>

      <div>
        <label htmlFor="experience" className="block mb-2 font-medium text-ink">
          How would you rate your level?
        </label>

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

      <div>
        <label htmlFor="telegramUsername" className="block mb-2 font-medium text-ink">
          Telegram (optional)
        </label>

        {errors.telegramUsername && errors.telegramUsername.type === 'maxLength' && (
          <div className="text-sm text-red-700 mt-3 mb-2">
            The username must be 50 characters or fewer.
          </div>
        )}

        <input
          type="text"
          {...register('telegramUsername', { maxLength: 50 })}
          id="telegramUsername"
          autoComplete="username"
          className="field"
        />

        <p className="mt-2 text-sm text-gray-500">
          Optional — add your Telegram handle if you prefer to chat there. Otherwise your mentor
          will reach out by email.
        </p>
      </div>

      <input type="hidden" {...register('captchaToken', { required: true })} id="captchaToken" />

      <Turnstile
        siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
        onSuccess={handleCaptchaOnSuccess}
        onExpire={handleCaptchaOnExpire}
        options={{ language: 'en' }}
      />

      {isError && (
        <div className="text-red-700">
          Something went wrong. We&apos;re probably already fixing it — please try again later.
        </div>
      )}

      <button className="button" type="submit" disabled={isLoading}>
        Send request
      </button>
    </form>
  )
}
