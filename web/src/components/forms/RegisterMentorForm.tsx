import Select, { type MultiValue, type StylesConfig } from 'react-select'
import Image from 'next/image'
import classNames from 'classnames'
import { useForm, Controller } from 'react-hook-form'
import { Turnstile } from '@marsidev/react-turnstile'
import Wysiwyg from './Wysiwyg'
import filters from '@/config/filters'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faQuestionCircle } from '@fortawesome/free-solid-svg-icons'
import { Tooltip } from 'react-tooltip'
import Link from 'next/link'
import { useState, useRef, type ChangeEvent, type DragEvent } from 'react'
import type { RegisterMentorRequest, ProfilePictureData } from '@/types/api'

interface TagOption {
  value: string
  label: string
}

interface RegisterFormData {
  name: string
  email: string
  contact: string
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

// react-select restyled to the design system: 1.5px line border with 12px
// radius, cobalt focus ring, navy chips (design 04 — tag multi-select).
const selectStyles: StylesConfig<TagOption, true> = {
  control: (base, state) => ({
    ...base,
    padding: '5px 6px',
    borderWidth: '1.5px',
    borderColor: state.isFocused ? '#2F5EFF' : '#DEDBD1', // cobalt / --om-line
    borderRadius: '12px',
    boxShadow: state.isFocused ? '0 0 0 3px rgb(47 94 255 / 0.13)' : 'none',
    transition: 'border-color 120ms ease-out, box-shadow 120ms ease-out',
    '&:hover': {
      borderColor: state.isFocused ? '#2F5EFF' : '#DEDBD1',
    },
  }),
  multiValue: (base) => ({
    ...base,
    borderRadius: '999px',
    backgroundColor: '#132A52', // brand navy
    transition: 'background-color 120ms ease-in-out',
  }),
  multiValueLabel: (base) => ({
    ...base,
    color: 'white',
    fontSize: '0.8125rem',
    fontWeight: 600,
    lineHeight: '1.25rem',
    padding: '0.1875rem 0.25rem 0.1875rem 0.625rem',
  }),
  multiValueRemove: (base) => ({
    ...base,
    color: 'white',
    borderRadius: '0 999px 999px 0',
    paddingRight: '0.5rem',
    '&:hover': {
      backgroundColor: '#0E2140', // navy, one step darker
      color: 'white',
    },
  }),
  option: (base, state) => ({
    ...base,
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
    padding: '0.5rem 0.75rem',
    color: state.isSelected ? 'white' : '#161A20', // ink
    backgroundColor: state.isSelected ? '#2F5EFF' : state.isFocused ? '#F7F6F2' : 'white',
    '&:active': {
      backgroundColor: '#EDEBE4', // surface-deep
    },
  }),
  menu: (base) => ({
    ...base,
    border: '1px solid #DEDBD1', // --om-line
    borderRadius: '12px',
    overflow: 'hidden',
    boxShadow: '0 16px 36px -12px rgb(19 42 82 / 0.25)', // shadow-dropdown
  }),
  input: (base) => ({
    ...base,
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
  }),
  placeholder: (base) => ({
    ...base,
    fontSize: '0.875rem',
    color: '#5B6270', // ink-soft
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

interface RailSection {
  id: string
  label: string
  complete: boolean
}

/**
 * Progress rail dot: outline while the section is incomplete, fills mint
 * with a 150ms scale pop once it completes (design 04 MOTION note).
 */
function RailDot({ complete, onNavy }: { complete: boolean; onNavy: boolean }): JSX.Element {
  return (
    <span className="relative h-2 w-2 flex-none" aria-hidden="true">
      <span
        className={classNames(
          'absolute inset-0 rounded-full border-2',
          complete ? 'border-transparent' : onNavy ? 'border-white/40' : 'border-line'
        )}
      />
      <span
        className={classNames(
          'absolute inset-0 rounded-full bg-brand-mint transition-transform duration-150 ease-pop',
          complete ? 'scale-100' : 'scale-0'
        )}
      />
    </span>
  )
}

export default function RegisterMentorForm({
  isLoading,
  isError,
  onSubmit,
}: RegisterMentorFormProps): JSX.Element {
  const {
    control,
    register,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<RegisterFormData>()

  const [selectedImage, setSelectedImage] = useState<File | null>(null)
  const [imagePreview, setImagePreview] = useState<string | null>(null)
  const [imageError, setImageError] = useState('')
  const [isDragOver, setIsDragOver] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [captchaToken, setCaptchaToken] = useState<string>('')

  const processImageFile = (file: File | undefined): void => {
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

  const handleImageChange = (e: ChangeEvent<HTMLInputElement>): void => {
    processImageFile(e.target.files?.[0])
  }

  const handleImageDrop = (e: DragEvent<HTMLLabelElement>): void => {
    e.preventDefault()
    setIsDragOver(false)
    processImageFile(e.dataTransfer.files?.[0])
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

  // ── Progress rail: best-effort section completion from form state ─────
  const values = watch()
  const hasRichText = (html?: string): boolean =>
    Boolean(html && html.replace(/<[^>]*>/g, '').trim())

  const sections: RailSection[] = [
    {
      id: 'reg-contact',
      label: 'Contact details',
      complete: Boolean(values.name?.trim()) && /^\S+@\S+$/i.test(values.email || ''),
    },
    {
      id: 'reg-profile',
      label: 'Your profile',
      complete: Boolean(
        selectedImage &&
          values.job?.trim() &&
          values.workplace?.trim() &&
          values.experience &&
          values.price
      ),
    },
    {
      id: 'reg-expertise',
      label: 'Your expertise',
      complete:
        (values.tags?.length ?? 0) >= 1 &&
        hasRichText(values.about) &&
        hasRichText(values.description) &&
        Boolean(values.competencies?.trim()),
    },
    {
      id: 'reg-scheduling',
      label: 'Scheduling',
      complete: Boolean(captchaToken) && isValidUrl(values.calendarUrl),
    },
  ]
  const currentIndex = sections.findIndex((section) => !section.complete)
  const completeCount = sections.filter((section) => section.complete).length

  const requiredText = 'This field is required.'

  const requiredMark = <span className="text-danger"> *</span>

  const fieldError = (message: string): JSX.Element => (
    <div className="mt-2 text-sm font-medium text-danger" role="alert">
      {message}
    </div>
  )

  const labelClass = 'mb-1.5 block text-sm font-semibold text-ink'
  const sectionHeadingClass = 'mb-0 text-[22px] leading-[1.1] tracking-[-0.01em]'

  return (
    <div className="lg:flex lg:items-start lg:gap-14">
      {/* ── Section rail: desktop-only, sticky (design 04) ─────────────── */}
      <nav
        aria-label="Form sections"
        className="hidden w-[220px] flex-none flex-col gap-0.5 lg:sticky lg:top-6 lg:flex"
      >
        <div className="mb-2.5 font-display text-[13px] font-extrabold uppercase tracking-[0.04em] text-ink">
          Become a mentor
        </div>
        {sections.map((section, index) => {
          const isCurrent = index === currentIndex
          return (
            <a
              key={section.id}
              href={`#${section.id}`}
              className={classNames(
                'flex items-center gap-2.5 rounded-[11px] px-3.5 py-[11px] text-[13px] transition-colors duration-120',
                isCurrent
                  ? 'bg-brand-navy font-semibold !text-white'
                  : 'font-medium text-ink-mute hover:bg-surface hover:text-ink'
              )}
            >
              <RailDot complete={section.complete} onNavy={isCurrent} />
              {section.label}
            </a>
          )
        })}
        <p className="mb-0 mt-3.5 px-3.5 text-xs leading-[1.55] text-ink-soft">
          Everything is editable later from your dashboard.
        </p>
      </nav>

      <div className="min-w-0 flex-1 lg:max-w-[620px]">
        {/* ── Mobile: rail becomes a slim progress bar (design 04 mobile) ── */}
        <div className="mb-7 lg:hidden" aria-hidden="true">
          <div className="flex gap-[5px]">
            {sections.map((section, index) => (
              <span
                key={section.id}
                className={classNames(
                  'h-[5px] flex-1 rounded-full transition-colors duration-180',
                  section.complete
                    ? 'bg-brand-mint'
                    : index === currentIndex
                    ? 'bg-brand-cobalt'
                    : 'bg-line'
                )}
              />
            ))}
          </div>
          <div className="meta-mono mt-1.5 text-[10px] text-ink-mute">
            {completeCount} of 4 sections complete
          </div>
        </div>

        <form className="space-y-9" onSubmit={handleSubmit(handleFormSubmit)}>
          <section id="reg-contact" className="scroll-mt-6 space-y-5">
            <h2 className={sectionHeadingClass}>1 · Contact details</h2>

            <div className="grid gap-5 sm:grid-cols-2">
              <div>
                <label htmlFor="name" className={labelClass}>
                  Your full name
                  {requiredMark}
                </label>

                <input
                  type="text"
                  {...register('name', { required: true, maxLength: 100 })}
                  id="name"
                  autoComplete="name"
                  className={classNames('field', errors.name && 'field-error')}
                />

                {errors.name && fieldError(requiredText)}
              </div>

              <div>
                <label htmlFor="email" className={labelClass}>
                  Your email
                  {requiredMark}{' '}
                  <span className="font-normal text-ink-soft">(never shown publicly)</span>
                </label>

                <input
                  type="email"
                  {...register('email', { required: true, pattern: /^\S+@\S+$/i, maxLength: 255 })}
                  id="email"
                  autoComplete="email"
                  className={classNames('field', errors.email && 'field-error')}
                />

                {errors.email && errors.email.type === 'required' && fieldError(requiredText)}
                {errors.email &&
                  errors.email.type === 'pattern' &&
                  fieldError('Please enter a valid email address.')}
              </div>
            </div>

            <div>
              <label htmlFor="contact" className={labelClass}>
                How can we reach you?
              </label>

              <input
                type="text"
                {...register('contact', { maxLength: 100 })}
                id="contact"
                className={classNames('field', errors.contact && 'field-error')}
              />

              {errors.contact && fieldError('The contact details must be 100 characters or fewer.')}

              <p className="mb-0 mt-2 text-sm text-ink-soft">
                Optional — email, Telegram, LinkedIn, whatever works for you. Otherwise we&apos;ll
                reach out by email.
              </p>
            </div>
          </section>

          <section id="reg-profile" className="scroll-mt-6 space-y-5">
            <h2 className={sectionHeadingClass}>2 · Your profile</h2>

            <div className="flex flex-col gap-5 sm:flex-row sm:items-start">
              <div className="w-full sm:w-[180px] sm:flex-none">
                <label htmlFor="profilePicture" className={labelClass}>
                  Profile photo
                  {requiredMark}
                </label>

                <input
                  ref={fileInputRef}
                  type="file"
                  id="profilePicture"
                  accept="image/jpeg,image/jpg,image/png,image/webp"
                  onChange={handleImageChange}
                  className="sr-only"
                />

                {/* Dropzone (design 04): dashed cobalt border, cobalt tint;
                    the photo lands with a 200ms scale-in preview. */}
                <label
                  htmlFor="profilePicture"
                  onDragOver={(e) => {
                    e.preventDefault()
                    setIsDragOver(true)
                  }}
                  onDragLeave={() => setIsDragOver(false)}
                  onDrop={handleImageDrop}
                  className={classNames(
                    'flex h-[190px] cursor-pointer flex-col items-center justify-center gap-2 rounded-card border-2 border-dashed p-3.5 text-center transition-colors duration-120',
                    isDragOver
                      ? 'border-brand-cobalt bg-brand-cobalt/[0.08]'
                      : 'border-brand-cobalt/50 bg-brand-cobalt/[0.04] hover:bg-brand-cobalt/[0.08]'
                  )}
                >
                  {imagePreview ? (
                    <Image
                      src={imagePreview}
                      alt="Preview"
                      className="h-[130px] w-[130px] animate-rise-in rounded-card object-cover"
                      unoptimized
                      width={130}
                      height={130}
                    />
                  ) : (
                    <>
                      <svg
                        width="26"
                        height="26"
                        viewBox="0 0 24 24"
                        fill="none"
                        aria-hidden="true"
                        className="text-brand-cobalt"
                      >
                        <path
                          d="M12 16V5M12 5l-4 4M12 5l4 4"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        />
                        <path
                          d="M4 17v2a1 1 0 001 1h14a1 1 0 001-1v-2"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                        />
                      </svg>
                      <span className="text-[13px] font-semibold text-brand-cobalt">
                        Upload photo
                      </span>
                      <span className="text-[11px] leading-[1.45] text-ink-soft">
                        Face forward, a light plain background works best. We&apos;ll cut it out
                        automatically.
                      </span>
                    </>
                  )}
                </label>

                <p className="meta-mono mb-0 mt-2 text-[10px] text-ink-mute">
                  JPEG · PNG · WebP · max 10 MB
                </p>

                {selectedImage && (
                  <div className="mt-2 flex items-baseline gap-2">
                    <span className="min-w-0 break-words text-xs text-mint-ink">
                      Photo selected: {selectedImage.name}
                    </span>
                    <button
                      type="button"
                      onClick={handleCancelImage}
                      className="text-xs font-semibold text-ink-soft underline hover:text-ink"
                    >
                      Cancel
                    </button>
                  </div>
                )}

                {imageError && fieldError(imageError)}
              </div>

              <div className="min-w-0 flex-1 space-y-5">
                <div>
                  <label htmlFor="job" className={labelClass}>
                    Job title
                    {requiredMark}
                  </label>

                  <input
                    type="text"
                    {...register('job', { required: true, maxLength: 200 })}
                    id="job"
                    autoComplete="organization-title"
                    className={classNames('field', errors.job && 'field-error')}
                  />

                  {errors.job && fieldError(requiredText)}
                </div>

                <div>
                  <label htmlFor="workplace" className={labelClass}>
                    Company
                    {requiredMark}{' '}
                    <a data-tooltip-id="workplace-tip">
                      <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
                    </a>
                    <Tooltip id="workplace-tip" place="right" className="z-50">
                      <span>
                        If you work in several places, name your main company and list the rest in
                        the &quot;About you&quot; section
                      </span>
                    </Tooltip>
                  </label>

                  <input
                    type="text"
                    {...register('workplace', { required: true, maxLength: 200 })}
                    id="workplace"
                    autoComplete="organization"
                    className={classNames('field', errors.workplace && 'field-error')}
                  />

                  {errors.workplace && fieldError(requiredText)}
                </div>
              </div>
            </div>

            <div className="grid gap-5 sm:grid-cols-2">
              <div>
                <label htmlFor="experience" className={labelClass}>
                  Experience
                  {requiredMark}
                </label>

                <select
                  {...register('experience', { required: true })}
                  id="experience"
                  className={classNames('field', errors.experience && 'field-error')}
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

                {errors.experience && fieldError(requiredText)}
              </div>

              <div>
                <label htmlFor="price" className={labelClass}>
                  Price per one-hour session
                  {requiredMark}
                </label>

                <select
                  {...register('price', { required: true })}
                  id="price"
                  className={classNames('field', errors.price && 'field-error')}
                >
                  <option value="">Select price</option>
                  {filters.price.map((item) => (
                    <option key={item} value={item}>
                      {item}
                    </option>
                  ))}
                </select>

                {errors.price && fieldError(requiredText)}
              </div>
            </div>
          </section>

          <section id="reg-expertise" className="scroll-mt-6 space-y-5">
            <h2 className={sectionHeadingClass}>3 · Your expertise</h2>

            <div>
              <label htmlFor="tags" className={labelClass}>
                Specialization
                {requiredMark}{' '}
                <a data-tooltip-id="tags-tip">
                  <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
                </a>
                <Tooltip id="tags-tip" place="right" className="z-50">
                  <span>
                    Pick your main current specialization plus any areas you know well and are
                    ready to help with.
                    <br />
                    Mentees will find you by these tags in the search block.
                    <br />
                    They will also be shown on your profile.
                    <br />
                    Choose at least 1 and at most 5 tags.
                  </span>
                </Tooltip>
              </label>

              <p className="mb-1.5 mt-0 text-[13px] text-ink-soft">
                Pick up to {MAX_TAGS} — these drive search and the category filters.
              </p>

              <Controller
                name="tags"
                control={control}
                defaultValue={[]}
                rules={{
                  required: true,
                  validate: (value) =>
                    (value.length >= 1 && value.length <= 5) || 'Select 1 to 5 tags',
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

              {errors.tags && errors.tags.type !== 'validate' && fieldError(requiredText)}
              {errors.tags && errors.tags.type === 'validate' && fieldError('Select 1 to 5 tags.')}
            </div>

            <div>
              <label htmlFor="about" className={labelClass}>
                About you
                {requiredMark}{' '}
                <a data-tooltip-id="about-tip">
                  <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
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

              {errors.about && fieldError(requiredText)}
            </div>

            <div>
              <label htmlFor="description" className={labelClass}>
                How can you help?
                {requiredMark}{' '}
                <a data-tooltip-id="description-tip">
                  <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
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
                    It&apos;s also great to mention which levels of mentees can come to you for
                    help: Junior-Middle-Senior, team leads, C-level executives, and so on. One line
                    is enough, for example: <em>I help Senior engineers and team leads.</em>
                  </span>
                </Tooltip>
              </label>

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

              {errors.description && fieldError(requiredText)}
            </div>

            <div>
              <label htmlFor="competencies" className={labelClass}>
                Skills and technologies (comma-separated)
                {requiredMark}{' '}
                <a data-tooltip-id="competencies-tip">
                  <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
                </a>
                <Tooltip id="competencies-tip" place="right" className="z-50">
                  <span>
                    List the skills you&apos;d like to consult on, separated by commas. For
                    example: JavaScript, React, Leadership, Code Review. Mentees will be able to
                    find you by them.
                  </span>
                </Tooltip>
              </label>

              <input
                type="text"
                {...register('competencies', { required: true, maxLength: 5000 })}
                id="competencies"
                className={classNames('field', errors.competencies && 'field-error')}
              />

              {errors.competencies && fieldError(requiredText)}
            </div>
          </section>

          <section id="reg-scheduling" className="scroll-mt-6 space-y-5">
            <h2 className={sectionHeadingClass}>4 · Scheduling</h2>

            <div>
              <label htmlFor="calendarUrl" className={labelClass}>
                Booking link to your calendar (
                <Link
                  href="https://koalendar.com"
                  target="_blank"
                  className="link"
                  rel="noreferrer"
                >
                  Koalendar
                </Link>
                ,{' '}
                <Link href="https://calendly.com" target="_blank" className="link" rel="noreferrer">
                  Calendly
                </Link>{' '}
                or anything else){' '}
                <a data-tooltip-id="calendar-tip">
                  <FontAwesomeIcon icon={faQuestionCircle} className="text-ink-soft" />
                </a>
                <Tooltip id="calendar-tip" place="right" className="z-50">
                  <span>
                    If you use a calendar scheduling tool, add your booking link here so mentees
                    can book a session with you directly. We recommend Calendly or Koalendar - they 
                    are integrated with our platform, and the booking form will
                    appear right after a mentee submits a request.
                  </span>
                </Tooltip>
              </label>

              <input
                type="text"
                {...register('calendarUrl', {
                  validate: {
                    checkUrl: isValidUrl,
                  },
                  maxLength: 500,
                })}
                id="calendarUrl"
                className={classNames('field', errors.calendarUrl && 'field-error')}
              />

              {errors.calendarUrl && fieldError('This must be a valid URL')}
            </div>

            <div>
              <Turnstile
                siteKey={process.env.NEXT_PUBLIC_TURNSTILE_SITE_KEY || ''}
                onSuccess={handleCaptchaOnSuccess}
                onExpire={handleCaptchaOnExpire}
                options={{ language: 'en' }}
              />

              {!captchaToken && errors.name && (
                <div className="mt-2 text-sm font-medium text-danger">
                  Please confirm that you are not a robot.
                </div>
              )}
            </div>
          </section>

          {isError && (
            <div className="rounded-field border-[1.5px] border-danger/40 bg-danger/5 px-4 py-3 text-sm font-medium text-danger">
              Something went wrong. We&apos;re probably already fixing it — please try again later.
            </div>
          )}

          <div className="flex flex-col gap-3 border-t border-line pt-6 sm:flex-row sm:items-center sm:gap-4">
            <button
              type="submit"
              className="button px-[30px] py-[15px] text-[15px]"
              disabled={isLoading || !captchaToken}
            >
              {isLoading ? (
                <>
                  <FontAwesomeIcon className="animate-spin" icon={faCircleNotch} />
                  <span className="ml-2">Submitting...</span>
                </>
              ) : (
                <span>Create my profile</span>
              )}
            </button>

            <span className="text-[13px] leading-[1.5] text-ink-soft">
              You&apos;ll confirm via a magic link — no password to remember.
            </span>
          </div>
        </form>
      </div>
    </div>
  )
}
