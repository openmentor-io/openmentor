import Select, { type MultiValue, type StylesConfig } from 'react-select'
import Image from 'next/image'
import { useForm, Controller } from 'react-hook-form'
import { Turnstile } from '@marsidev/react-turnstile'
import Wysiwyg from './Wysiwyg'
import filters from '@/config/filters'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faQuestionCircle } from '@fortawesome/free-solid-svg-icons'
import { Tooltip } from 'react-tooltip'
import Link from 'next/link'
import { useState, useRef, type ChangeEvent } from 'react'
import type { RegisterMentorRequest, ProfilePictureData } from '@/types/api'

interface TagOption {
  value: string
  label: string
}

interface RegisterFormData {
  name: string
  email: string
  telegram: string
  job: string
  workplace: string
  experience: string
  price: string
  tags: string[]
  about: string
  description: string
  competencies: string
  calendarUrl?: string
}

interface RegisterMentorFormProps {
  isLoading: boolean
  isError: boolean
  onSubmit: (data: RegisterMentorRequest) => void
}

// Custom styles for react-select to match the previous Multiselect styling
const selectStyles: StylesConfig<TagOption, true> = {
  control: (base) => ({
    ...base,
    padding: '0.25rem 0.5rem',
    borderColor: '#DEDBD1', // --om-line
    borderRadius: '0.75rem',
    boxShadow: 'none',
    '&:hover': {
      borderColor: '#DEDBD1',
    },
  }),
  multiValue: (base) => ({
    ...base,
    borderRadius: '1rem',
    backgroundColor: '#132A52', // brand navy
  }),
  multiValueLabel: (base) => ({
    ...base,
    color: 'white',
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
    padding: '0.125rem 0.5rem',
  }),
  multiValueRemove: (base) => ({
    ...base,
    color: 'white',
    '&:hover': {
      backgroundColor: '#0E2140', // navy, one step darker
      color: 'white',
    },
  }),
  option: (base) => ({
    ...base,
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
    padding: '0.5rem 0.75rem',
  }),
  menu: (base) => ({
    ...base,
    border: '1px solid #DEDBD1', // --om-line
    borderRadius: '0.75rem',
    overflow: 'hidden',
  }),
  input: (base) => ({
    ...base,
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
  }),
}

// Convert string array to option array for react-select
const tagsToOptions = (tags: string[]): TagOption[] =>
  tags.map((tag) => ({ value: tag, label: tag }))

// All available tags as options
const tagOptions = tagsToOptions(filters.tags)
const MAX_TAGS = 5

function isValidUrl(value?: string): boolean {
  if (!value) return true
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

export default function RegisterMentorForm({
  isLoading,
  isError,
  onSubmit,
}: RegisterMentorFormProps): JSX.Element {
  const {
    control,
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RegisterFormData>()

  const [selectedImage, setSelectedImage] = useState<File | null>(null)
  const [imagePreview, setImagePreview] = useState<string | null>(null)
  const [imageError, setImageError] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [captchaToken, setCaptchaToken] = useState<string>('')

  const handleImageChange = (e: ChangeEvent<HTMLInputElement>): void => {
    const file = e.target.files?.[0]
    setImageError('') // Clear any previous errors

    if (!file) return

    // Validate file type
    const allowedTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp']
    if (!allowedTypes.includes(file.type)) {
      setImageError('Please choose a JPEG, PNG, or WebP image.')
      return
    }

    // Validate file size (max 10MB)
    const maxSize = 10 * 1024 * 1024 // 10MB
    if (file.size > maxSize) {
      setImageError('The file size must not exceed 10 MB.')
      return
    }

    setSelectedImage(file)

    // Create preview
    const reader = new FileReader()
    reader.onloadend = () => {
      setImagePreview(reader.result as string)
    }
    reader.readAsDataURL(file)
  }

  const handleCancelImage = (): void => {
    setSelectedImage(null)
    setImagePreview(null)
    setImageError('')
    if (fileInputRef.current) {
      fileInputRef.current.value = ''
    }
  }

  const handleCaptchaOnSuccess = (token: string): void => {
    setCaptchaToken(token)
  }

  const handleCaptchaOnExpire = (): void => {
    setCaptchaToken('')
  }

  const handleFormSubmit = (data: RegisterFormData): void => {
    if (!selectedImage || !imagePreview) {
      setImageError('Please choose a profile photo.')
      return
    }

    if (!captchaToken) {
      return
    }

    const profilePicture: ProfilePictureData = {
      image: imagePreview,
      fileName: selectedImage.name,
      contentType: selectedImage.type,
    }

    onSubmit({ ...data, profilePicture, captchaToken })
  }

  const requiredText = 'This field is required.'

  const requiredMark = <span className="text-sm text-red-700 mt-3 mb-2"> *</span>

  return (
    <form className="space-y-10" onSubmit={handleSubmit(handleFormSubmit)}>
      <section className="space-y-8">
        <h2 className="mb-0 text-xl font-semibold tracking-tight">Contact details</h2>

        <div>
          <label htmlFor="name" className="block mb-2 font-medium text-ink">
            Your full name
            {requiredMark}
          </label>

          {errors.name && <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>}

          <input
            type="text"
            {...register('name', { required: true, maxLength: 100 })}
            id="name"
            autoComplete="name"
            className="field"
          />
        </div>

        <div>
          <label htmlFor="email" className="block mb-2 font-medium text-ink">
            Your email
            {requiredMark}
          </label>

          {errors.email && errors.email.type === 'required' && (
            <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
          )}

          {errors.email && errors.email.type === 'pattern' && (
            <div className="text-sm text-red-700 mt-3 mb-2">
              Please enter a valid email address.
            </div>
          )}

          <input
            type="email"
            {...register('email', { required: true, pattern: /^\S+@\S+$/i, maxLength: 255 })}
            id="email"
            autoComplete="email"
            className="field"
          />
        </div>

        <div>
          <label htmlFor="telegram" className="block mb-2 font-medium text-ink">
            Telegram (optional)
          </label>

          {errors.telegram && (
            <div className="text-sm text-red-700 mt-3 mb-2">
              The username must be 50 characters or fewer.
            </div>
          )}

          <input
            type="text"
            {...register('telegram', { maxLength: 50 })}
            id="telegram"
            autoComplete="username"
            className="field"
          />

          <p className="mt-2 text-sm text-gray-500">
            Optional — add your Telegram handle if you prefer to chat there. Otherwise we&apos;ll
            reach out by email. Enter just the username, without @ or links.
          </p>
        </div>
      </section>

      <section className="space-y-8 border-t border-line pt-8">
        <h2 className="mb-0 text-xl font-semibold tracking-tight">Your profile</h2>

        <div>
          <label htmlFor="profilePicture" className="block mb-2 font-medium text-ink">
            Profile photo
            {requiredMark}{' '}
            <a data-tooltip-id="photo-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="photo-tip" place="right" className="z-50">
              <span>
                Upload your profile photo. JPEG, PNG, and WebP formats are supported. The maximum
                file size is 10 MB.
              </span>
            </Tooltip>
          </label>

          <div className="mt-2 space-y-4">
            {imagePreview && (
              <div className="flex items-center space-x-4">
                <Image
                  src={imagePreview}
                  alt="Preview"
                  className="w-24 h-24 rounded-full object-cover"
                  unoptimized
                  width={96}
                  height={96}
                />
                <span className="text-sm text-gray-600">Preview</span>
              </div>
            )}

            <div className="flex items-center space-x-4">
              <input
                ref={fileInputRef}
                type="file"
                id="profilePicture"
                accept="image/jpeg,image/jpg,image/png,image/webp"
                onChange={handleImageChange}
                className="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded-md file:border-0 file:text-sm file:font-medium file:bg-brand-cobalt/10 file:text-brand-cobalt hover:file:bg-brand-cobalt/20"
              />
            </div>

            {selectedImage && (
              <div className="flex items-center space-x-4">
                <span className="text-sm text-green-700">Photo selected: {selectedImage.name}</span>
                <button
                  type="button"
                  onClick={handleCancelImage}
                  className="text-sm text-gray-600 hover:text-gray-800"
                >
                  Cancel
                </button>
              </div>
            )}

            {imageError && (
              <div className="text-sm text-red-700" role="alert">
                {imageError}
              </div>
            )}
          </div>
        </div>

        <div>
          <label htmlFor="job" className="block mb-2 font-medium text-ink">
            Job title
            {requiredMark}
          </label>

          {errors.job && <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>}

          <input
            type="text"
            {...register('job', { required: true, maxLength: 200 })}
            id="job"
            autoComplete="organization-title"
            className="field"
          />
        </div>

        <div>
          <label htmlFor="workplace" className="block mb-2 font-medium text-ink">
            Company
            {requiredMark}{' '}
            <a data-tooltip-id="workplace-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="workplace-tip" place="right" className="z-50">
              <span>
                If you work in several places, name your main company and list the rest in the
                &quot;About you&quot; section
              </span>
            </Tooltip>
          </label>

          {errors.workplace && <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>}

          <input
            type="text"
            {...register('workplace', { required: true, maxLength: 200 })}
            id="workplace"
            autoComplete="organization"
            className="field"
          />
        </div>

        <div className="grid gap-6 sm:grid-cols-2">
          <div>
            <label htmlFor="experience" className="block mb-2 font-medium text-ink">
              Experience
              {requiredMark}
            </label>

            <select
              {...register('experience', { required: true })}
              id="experience"
              className="field"
            >
              <option value="">Select experience</option>
              {Object.keys(filters.experience).map((item) => (
                <option
                  key={filters.experience[item as keyof typeof filters.experience]}
                  value={filters.experience[item as keyof typeof filters.experience]}
                >
                  {item}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor="price" className="block mb-2 font-medium text-ink">
              Price per one-hour session
              {requiredMark}
            </label>

            <select {...register('price', { required: true })} id="price" className="field">
              <option value="">Select price</option>
              {filters.price.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </div>
        </div>
      </section>

      <section className="space-y-8 border-t border-line pt-8">
        <h2 className="mb-0 text-xl font-semibold tracking-tight">Your expertise</h2>

        <div>
          <label htmlFor="tags" className="block mb-2 font-medium text-ink">
            Specialization
            {requiredMark}{' '}
            <a data-tooltip-id="tags-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="tags-tip" place="right" className="z-50">
              <span>
                Pick your main current specialization plus any areas you know well and are ready to
                help with.
                <br />
                Mentees will find you by these tags in the search block.
                <br />
                They will also be shown on your profile.
                <br />
                Choose at least 1 and at most 5 tags.
              </span>
            </Tooltip>
          </label>

          {errors.tags && <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>}

          <Controller
            name="tags"
            control={control}
            defaultValue={[]}
            rules={{
              required: true,
              validate: (value) => (value.length >= 1 && value.length <= 5) || 'Select 1 to 5 tags',
            }}
            render={({ field }) => (
              <Select<TagOption, true>
                isMulti
                value={tagsToOptions(field.value || [])}
                onChange={(newValue: MultiValue<TagOption>) => {
                  if (newValue.length <= MAX_TAGS) {
                    field.onChange(newValue.map((opt) => opt.value))
                  }
                }}
                options={tagOptions}
                closeMenuOnSelect={false}
                placeholder="Select tags..."
                noOptionsMessage={() => 'No options available'}
                styles={selectStyles}
                classNamePrefix="react-select"
              />
            )}
          />

          {errors.tags && errors.tags.type === 'validate' && (
            <div className="text-sm text-red-700 mt-2">Select 1 to 5 tags.</div>
          )}
        </div>

        <div>
          <label htmlFor="about" className="block mb-2 font-medium text-ink">
            About you
            {requiredMark}{' '}
            <a data-tooltip-id="about-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="about-tip" place="right" className="z-50">
              <span>
                Two or three paragraphs work best: where you&apos;ve worked, what interests you
                professionally,
                <br />
                and what mentoring approach you follow.
              </span>
            </Tooltip>
          </label>

          {errors.about && <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>}

          <div className="mt-1">
            <Controller
              name="about"
              control={control}
              defaultValue=""
              rules={{ required: true }}
              render={({ field }) => (
                <Wysiwyg
                  content={field.value}
                  onUpdate={(editor) => field.onChange(editor.getHTML())}
                />
              )}
            />
          </div>
        </div>

        <div>
          <label htmlFor="description" className="block mb-2 font-medium text-ink">
            How can you help?
            {requiredMark}{' '}
            <a data-tooltip-id="description-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="description-tip" place="right" className="z-50">
              <span>
                It&apos;s best to break the text into bullet points. For example,
                <br />
              </span>
              <em>
                <span>I can help you:</span>
                <ul>
                  <li>— get comfortable with Kubernetes;</li>
                  <li>— improve your team&apos;s processes;</li>
                  <li>— choose the right strategy to grow your startup;</li>
                </ul>
              </em>
              <br />
              <span>
                It&apos;s also great to mention which levels of mentees can come to you for help:
                Junior-Middle-Senior, team leads, C-level executives, and so on. One line is enough,
                for example: <em>I help Senior engineers and team leads.</em>
              </span>
            </Tooltip>
          </label>

          {errors.description && (
            <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
          )}

          <div className="mt-1">
            <Controller
              name="description"
              control={control}
              defaultValue=""
              rules={{ required: true }}
              render={({ field }) => (
                <Wysiwyg
                  content={field.value}
                  onUpdate={(editor) => field.onChange(editor.getHTML())}
                />
              )}
            />
          </div>
        </div>

        <div>
          <label htmlFor="competencies" className="block mb-2 font-medium text-ink">
            Skills and technologies (comma-separated)
            {requiredMark}{' '}
            <a data-tooltip-id="competencies-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="competencies-tip" place="right" className="z-50">
              <span>
                List the skills you&apos;d like to consult on, separated by commas. For example:
                JavaScript, React, Leadership, Code Review. Mentees will be able to find you by
                them.
              </span>
            </Tooltip>
          </label>

          {errors.competencies && (
            <div className="text-sm text-red-700 mt-3 mb-2">{requiredText}</div>
          )}

          <input
            type="text"
            {...register('competencies', { required: true, maxLength: 5000 })}
            id="competencies"
            className="field"
          />
        </div>
      </section>

      <section className="space-y-8 border-t border-line pt-8">
        <h2 className="mb-0 text-xl font-semibold tracking-tight">Scheduling</h2>

        <div>
          <label htmlFor="calendarUrl" className="block mb-2 font-medium text-ink">
            Booking link to your calendar (
            <Link
              href="https://calendlab.ru/signup?referral_code=for-mentors-6-months"
              target="_blank"
              className="link"
              rel="noreferrer"
            >
              CalendLab
            </Link>
            ,{' '}
            <Link href="https://koalendar.com" target="_blank" className="link" rel="noreferrer">
              Koalendar
            </Link>
            ,{' '}
            <Link href="https://calendly.com" target="_blank" className="link" rel="noreferrer">
              Calendly
            </Link>{' '}
            or anything else){' '}
            <a data-tooltip-id="calendar-tip">
              <FontAwesomeIcon icon={faQuestionCircle} />
            </a>
            <Tooltip id="calendar-tip" place="right" className="z-50">
              <span>
                If you use a calendar scheduling tool, add your booking link here so mentees can
                book a session with you directly. We recommend Calendly, Koalendar, or CalendLab —
                they are integrated with our platform, and the booking form will appear right after
                a mentee submits a request.
              </span>
            </Tooltip>
          </label>

          {errors.calendarUrl && (
            <div className="text-sm text-red-700 mt-3 mb-2">This must be a valid URL</div>
          )}

          <input
            type="text"
            {...register('calendarUrl', {
              validate: {
                checkUrl: isValidUrl,
              },
              maxLength: 500,
            })}
            id="calendarUrl"
            className="field"
          />

          <label htmlFor="calendarUrl" className="block mb-2 mt-1 font-small italic text-gray-700">
            🎉 You can get your first 6 months of CalendLab for free via{' '}
            <Link
              href="https://calendlab.ru/signup?referral_code=for-mentors-6-months"
              target="_blank"
              className="link"
              rel="noreferrer"
            >
              this link
            </Link>
            .
          </label>
        </div>
      </section>

      <div>
        <Turnstile
          siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
          onSuccess={handleCaptchaOnSuccess}
          onExpire={handleCaptchaOnExpire}
          options={{ language: 'en' }}
        />

        {!captchaToken && errors.name && (
          <div className="text-sm text-red-700 mt-2">Please confirm that you are not a robot.</div>
        )}
      </div>

      {isError && (
        <div className="text-red-700">
          Something went wrong. We&apos;re probably already fixing it — please try again later.
        </div>
      )}

      <button type="submit" className="button" disabled={isLoading || !captchaToken}>
        {isLoading ? (
          <>
            <FontAwesomeIcon className="animate-spin" icon={faCircleNotch} />
            <span className="ml-2">Submitting...</span>
          </>
        ) : (
          <span>Submit application</span>
        )}
      </button>
    </form>
  )
}
