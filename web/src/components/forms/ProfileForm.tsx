import Select, { type MultiValue, type StylesConfig } from 'react-select'
import Image from 'next/image'
import { useForm, Controller } from 'react-hook-form'
import Wysiwyg from './Wysiwyg'
import filters from '@/config/filters'
import { MAX_IMAGE_FILE_BYTES, MAX_IMAGE_FILE_LABEL } from '@/config/uploads'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faQuestionCircle } from '@fortawesome/free-solid-svg-icons'
import { Tooltip } from 'react-tooltip'
import Link from 'next/link'
import { useState, useRef, type ChangeEvent } from 'react'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import { isValidCalendarUrl } from '@/lib/safe-url'
import { formatPrice, isValidPrice, parsePrice, type PriceValue } from '@/lib/price'
import PriceField from './PriceField'
import type { MentorWithSecureFields } from '@/types'

interface TagOption {
  value: string
  label: string
}

interface ProfileFormData {
  name: string
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

/**
 * What the form holds internally. It differs from what it submits in exactly
 * one field: price is edited as a {kind, amount} pair and serialized to its
 * canonical string on submit, because "fixed with no amount yet" is a state the
 * string has no spelling for.
 */
type ProfileFormValues = Omit<ProfileFormData, 'price'> & { price: PriceValue | null }

interface ImageUploadData {
  image: string
  fileName: string
  contentType: string
}

type ImageUploadStatus = 'idle' | 'loading' | 'success' | 'error'

interface ProfileFormProps {
  mentor: MentorWithSecureFields
  isLoading: boolean
  isError: boolean
  onSubmit: (data: ProfileFormData) => void
  onImageUpload: (data: ImageUploadData, onSuccess: () => void) => void
  imageUploadStatus: ImageUploadStatus
  tempImagePreview: string | null
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
      borderColor: '#DEDBD1', // --om-line
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

type SelectOption = [value: string, label: string]

/**
 * `experience` is a TEXT column while this select offers only a few
 * suggestions. An uncontrolled <select> whose defaultValue matches no <option>
 * reports the FIRST option instead, which rewrote a stored "15 years" to "2-5"
 * on any save — so keep the stored value as an option of its own and it
 * round-trips untouched (D44).
 *
 * `price` used this too, and no longer needs to: PriceField has no "first
 * option" to fall into, and an unparseable stored price leaves it unselected
 * rather than silently becoming a real one (D87).
 */
function withStoredOption(
  options: SelectOption[],
  stored: string | null | undefined
): SelectOption[] {
  const value = stored ?? ''
  if (options.some(([optionValue]) => optionValue === value)) return options
  // An unset column becomes a placeholder the (required) field rejects, rather
  // than saving whichever suggestion happens to be listed first.
  return [[value, value || 'Please choose one'], ...options]
}

export default function ProfileForm({
  mentor,
  isLoading,
  isError,
  onSubmit,
  onImageUpload,
  imageUploadStatus,
  tempImagePreview,
}: ProfileFormProps): JSX.Element {
  const {
    control,
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ProfileFormValues>()

  const [selectedImage, setSelectedImage] = useState<File | null>(null)
  const [imagePreview, setImagePreview] = useState<string | null>(null)
  const [imageError, setImageError] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  const experienceOptions = withStoredOption(
    Object.entries(filters.experience).map(([label, value]): SelectOption => [value, label]),
    mentor.experience
  )
  // An unparseable stored price stays unselected rather than being coerced to a
  // real one: silently rewriting what a mentor charges is the D44 failure, and
  // the `required` rule turns it into a save the mentor has to resolve instead.
  const storedPrice = parsePrice(mentor.price)

  const MAX_TAGS = 5

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

    // The limit is the RAW file; the request that carries it is ~4/3 bigger
    // because the photo goes out base64-encoded inside JSON. See config/uploads.
    if (file.size > MAX_IMAGE_FILE_BYTES) {
      setImageError(`The file size must not exceed ${MAX_IMAGE_FILE_LABEL}.`)
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

  const handleImageUploadClick = async (): Promise<void> => {
    if (!selectedImage) {
      setImageError('Please choose an image to upload.')
      return
    }

    const reader = new FileReader()
    reader.onloadend = () => {
      const base64Image = reader.result as string
      onImageUpload(
        {
          image: base64Image,
          fileName: selectedImage.name,
          contentType: selectedImage.type,
        },
        () => {
          // Success callback - clear the selected image and preview
          setSelectedImage(null)
          setImagePreview(null)
          if (fileInputRef.current) {
            fileInputRef.current.value = ''
          }
        }
      )
    }
    reader.readAsDataURL(selectedImage)
  }

  const handleCancelImage = (): void => {
    setSelectedImage(null)
    setImagePreview(null)
    setImageError('')
    if (fileInputRef.current) {
      fileInputRef.current.value = ''
    }
  }

  const handleFormSubmit = (values: ProfileFormValues): void => {
    const price = values.price === null ? null : formatPrice(values.price)
    // The Controller's rule already blocks this, so reaching here means the
    // rule and formatPrice disagree — drop the save rather than post a price
    // the mentor did not choose.
    if (price === null) return
    onSubmit({ ...values, price })
  }

  return (
    <form className="space-y-8" onSubmit={handleSubmit(handleFormSubmit)}>
      <div>
        <label htmlFor="name" className="block mb-2 font-medium text-ink">
          Your full name
        </label>

        {errors.name && (
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <input
          type="text"
          {...register('name', { required: true })}
          defaultValue={mentor.name}
          id="name"
          autoComplete="name"
          className="field"
        />
      </div>

      <div>
        <label htmlFor="profilePicture" className="block mb-2 font-medium text-ink">
          Profile photo{' '}
          <a data-tooltip-id="photo-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="photo-tip" place="right">
            <span>
              Upload your profile photo. JPEG, PNG, and WebP formats are supported. The maximum file
              size is {MAX_IMAGE_FILE_LABEL}.
              <br />
              This feature is still experimental. If something goes wrong, drop us a line at
              hello@openmentor.io.
            </span>
          </Tooltip>
        </label>

        <div className="mt-2 space-y-4">
          {(mentor.photo_url || tempImagePreview) && !imagePreview && (
            <div className="flex items-center space-x-4">
              {tempImagePreview ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={tempImagePreview}
                  alt="Current profile"
                  className="w-24 h-24 rounded-full object-cover"
                />
              ) : (
                <Image
                  src={imageLoader({
                    src: mentor.mentorId,
                    quality: 'full',
                    version: updatedAtToVersion(mentor.updatedAt),
                  })}
                  alt="Current profile"
                  className="w-24 h-24 rounded-full object-cover"
                  width={40}
                  height={40}
                  unoptimized
                  key={mentor.updatedAt ?? mentor.photo_url}
                />
              )}
              <span className="text-sm text-ink-soft">Current photo</span>
            </div>
          )}

          {imagePreview && (
            <div className="flex items-center space-x-4">
              <Image
                src={imagePreview}
                alt="Preview"
                className="w-24 h-24 rounded-full object-cover"
                unoptimized
                width={40}
                height={40}
              />
              <span className="text-sm text-ink-soft">Preview</span>
            </div>
          )}

          <div className="flex items-center space-x-4">
            <input
              ref={fileInputRef}
              type="file"
              id="profilePicture"
              accept="image/jpeg,image/jpg,image/png,image/webp"
              onChange={handleImageChange}
              className="block w-full text-sm text-ink-soft file:mr-4 file:py-2 file:px-4 file:rounded-field file:border-0 file:text-sm file:font-medium file:bg-brand-cobalt/10 file:text-brand-cobalt hover:file:bg-brand-cobalt/20"
            />
          </div>

          {selectedImage && (
            <div className="flex items-center space-x-4">
              <button
                type="button"
                onClick={handleImageUploadClick}
                className="button"
                disabled={imageUploadStatus === 'loading'}
              >
                {imageUploadStatus === 'loading' ? (
                  <>
                    <FontAwesomeIcon className="animate-spin" icon={faCircleNotch} />
                    <span className="ml-2">Uploading...</span>
                  </>
                ) : (
                  <span>Upload photo</span>
                )}
              </button>

              <button
                type="button"
                onClick={handleCancelImage}
                className="text-sm text-ink-soft hover:text-ink"
                disabled={imageUploadStatus === 'loading'}
              >
                Cancel
              </button>
            </div>
          )}

          {imageUploadStatus === 'success' && (
            <div className="text-sm text-mint-ink">
              Photo uploaded successfully! Your profile will be updated shortly.
            </div>
          )}

          {imageUploadStatus === 'error' && (
            <div className="text-sm text-danger">
              Failed to upload the photo. Please try again.
            </div>
          )}

          {imageError && (
            <div className="text-sm text-danger" role="alert">
              {imageError}
            </div>
          )}
        </div>
      </div>

      <div>
        <label htmlFor="job" className="block mb-2 font-medium text-ink">
          Job title
        </label>

        {errors.job && (
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <input
          type="text"
          {...register('job', { required: true })}
          defaultValue={mentor.job}
          id="job"
          autoComplete="organization-title"
          className="field"
        />
      </div>

      <div>
        <label htmlFor="workplace" className="block mb-2 font-medium text-ink">
          Company{' '}
          <a data-tooltip-id="workplace-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="workplace-tip" place="right">
            <span>
              If you work in several places, name your main company and list the rest in the
              &quot;About you&quot; section
            </span>
          </Tooltip>
        </label>

        {errors.workplace && (
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <input
          type="text"
          {...register('workplace', { required: true })}
          defaultValue={mentor.workplace}
          id="workplace"
          autoComplete="organization"
          className="field"
        />
      </div>

      <div className="flex space-x-8">
        <div>
          <label htmlFor="experience" className="block mb-2 font-medium text-ink">
            Experience
          </label>

          {errors.experience && (
            <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
          )}

          <select
            {...register('experience', { required: true })}
            defaultValue={mentor.experience}
            id="experience"
            className="field"
          >
            {experienceOptions.map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <Controller
            control={control}
            name="price"
            defaultValue={storedPrice}
            rules={{ validate: (value) => value !== null && isValidPrice(value) }}
            render={({ field }) => (
              <PriceField
                value={field.value}
                onChange={field.onChange}
                inputRef={field.ref}
                label="Price per one-hour session"
                labelClassName="block mb-2 font-medium text-ink"
                invalid={Boolean(errors.price)}
                error={
                  errors.price
                    ? 'Choose Free, Negotiable, or a whole amount from $1 to $1000.'
                    : undefined
                }
              />
            )}
          />
        </div>
      </div>

      <div>
        <label htmlFor="tags" className="block mb-2 font-medium text-ink">
          Specialization{' '}
          <a data-tooltip-id="tags-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="tags-tip" place="right">
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

        <Controller
          name="tags"
          control={control}
          defaultValue={mentor.tags}
          render={({ field }) => (
            <Select<TagOption, true>
              isMulti
              value={tagsToOptions(field.value || [])}
              onChange={(newValue: MultiValue<TagOption>) => {
                if (newValue.length < field.value.length || newValue.length <= MAX_TAGS) {
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
      </div>

      <div>
        <label htmlFor="about" className="block mb-2 font-medium text-ink">
          About you{' '}
          <a data-tooltip-id="about-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="about-tip" place="right">
            <span>
              Two or three paragraphs work best: where you&apos;ve worked, what interests you
              professionally, and what mentoring approach you follow
            </span>
          </Tooltip>
        </label>

        {errors.about && (
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <div className="mt-1">
          <Controller
            name="about"
            control={control}
            defaultValue={mentor.about || ''}
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
          How can you help?{' '}
          <a data-tooltip-id="description-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="description-tip" place="right">
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
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <div className="mt-1">
          <Controller
            name="description"
            control={control}
            defaultValue={mentor.description || ''}
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
          Skills and technologies (comma-separated){' '}
          <a data-tooltip-id="competencies-tip">
            <FontAwesomeIcon icon={faQuestionCircle} />
          </a>
          <Tooltip id="competencies-tip" place="right">
            <span>
              List the skills you&apos;d like to consult on, separated by commas. For example:
              JavaScript, React, Leadership, Code Review. Mentees will be able to find you by them.
            </span>
          </Tooltip>
        </label>

        {errors.competencies && (
          <div className="text-sm text-danger mt-3 mb-2">This field is required.</div>
        )}

        <input
          type="text"
          {...register('competencies', { required: true })}
          defaultValue={mentor.competencies}
          id="competencies"
          className="field"
        />
      </div>

      <div>
        <label htmlFor="calendarUrl" className="block mb-2 font-medium text-ink">
          Booking link to your calendar (
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
          <Tooltip id="calendar-tip" place="right">
            <span>
              If you use a calendar scheduling tool, add your booking link here so mentees can book
              a session with you directly. We recommend Calendly, Koalendar, or CalendLab — they are
              integrated with our platform, and the booking form will appear right after a mentee
              submits a request.
            </span>
          </Tooltip>
        </label>

        {errors.calendarUrl && (
          <div className="text-sm text-danger mt-3 mb-2">This must be a valid https:// URL</div>
        )}

        <input
          type="text"
          {...register('calendarUrl', {
            // Trim first: a pasted booking link often carries surrounding
            // whitespace, which the API's https_url tag rejects.
            setValueAs: (value: string) => value.trim(),
            validate: {
              checkUrl: isValidCalendarUrl,
            },
          })}
          defaultValue={mentor.calendarUrl || ''}
          id="calendarUrl"
          className="field"
        />
      </div>

      {isError && (
        <div className="text-danger">
          Something went wrong. We&apos;re probably already fixing it — please try saving again
          later.
        </div>
      )}

      <button type="submit" className="button" disabled={isLoading}>
        {isLoading ? (
          <>
            <FontAwesomeIcon className="animate-spin" icon={faCircleNotch} />
            <span className="ml-2">Saving...</span>
          </>
        ) : (
          <span>Save</span>
        )}
      </button>
    </form>
  )
}
