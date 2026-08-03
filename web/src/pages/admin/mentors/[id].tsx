import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/router'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import { AuthGateScreen, NoIndexHead } from '@/components'
import {
  AdminAuthProvider,
  AdminLayout,
  useAdminAuth,
  moderationStatusBadgeClass,
} from '@/components/admin-moderation'
import Wysiwyg from '@/components/forms/Wysiwyg'
import filters from '@/config/filters'
import type {
  AdminMentorDetails,
  AdminMentorProfileUpdateRequest,
  UploadProfilePictureRequest,
} from '@/types'
import {
  getModerationMentorById,
  updateModerationMentor,
  approveModerationMentor,
  declineModerationMentor,
  returnModerationMentor,
  updateModerationMentorStatus,
  uploadModerationMentorPicture,
  changeModerationMentorUsername,
  ApiError,
} from '@/lib/admin-moderation-api'
import { imageLoader } from '@/lib/image-loader'
import { isValidCalendarUrl } from '@/lib/safe-url'
import {
  useUsernameAvailability,
  isUsernameFormatValid,
  USERNAME_MAX_LENGTH,
} from '@/lib/username'

type SaveState = 'idle' | 'loading' | 'success' | 'error'
type PictureState = 'idle' | 'loading' | 'success' | 'error'

const RETURN_REASON_MAX = 2000

function getBackLink(status: AdminMentorDetails['status']): string {
  if (status === 'pending' || status === 'draft') return '/admin/mentors/pending'
  if (status === 'declined') return '/admin/mentors/declined'
  return '/admin/mentors/approved'
}

function buildFormData(mentor: AdminMentorDetails): AdminMentorProfileUpdateRequest {
  return {
    name: mentor.name,
    email: mentor.email,
    contact: mentor.contact,
    job: mentor.job,
    workplace: mentor.workplace,
    experience: mentor.experience,
    price: mentor.price,
    tags: mentor.tags,
    about: mentor.about,
    description: mentor.description,
    competencies: mentor.competencies,
    calendarUrl: mentor.calendarUrl || '',
  }
}

const labelClass = 'mb-1 block text-[13px] font-semibold text-ink'

/**
 * Admin username (slug) change block. Separate from the profile save form:
 * a rename is a breaking action (old links redirect for 2 hops, then die) and
 * goes through the dedicated history-aware endpoint. Admins have no cooldown,
 * but availability is still enforced. Two-step inline confirm mirrors the
 * decline flow elsewhere on this page.
 */
function AdminUsernameField({
  mentorId,
  currentUsername,
  onChanged,
}: {
  mentorId: string
  currentUsername: string
  onChanged: (mentor: AdminMentorDetails) => void
}): JSX.Element {
  const [value, setValue] = useState(currentUsername)
  const [confirming, setConfirming] = useState(false)
  const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle')
  const [error, setError] = useState<string | null>(null)

  // Keep the input in sync when the mentor reloads (e.g. after a save).
  useEffect(() => {
    setValue(currentUsername)
    setConfirming(false)
    setState('idle')
    setError(null)
  }, [currentUsername])

  // Encode the id path segment: mentorId comes from the route param, so
  // encoding prevents a crafted value ("../…") from redirecting the
  // availability fetch to a different endpoint (CWE-918 / request forgery).
  const availabilityEndpoint = `/api/admin/mentors/${encodeURIComponent(mentorId)}/username`
  const availability = useUsernameAvailability(value, availabilityEndpoint)
  const isUnchanged = value === currentUsername
  const canSubmit =
    !isUnchanged &&
    isUsernameFormatValid(value) &&
    (availability.status === 'available' ||
      // The mentor's own current name reads as available; unchanged is blocked above.
      availability.status === 'idle') &&
    state !== 'loading'

  const onConfirm = async (): Promise<void> => {
    setState('loading')
    setError(null)
    try {
      await changeModerationMentorUsername(mentorId, value)
      // Re-fetch to get the canonical mentor (slug + updatedAt) after the change.
      const refreshed = await getModerationMentorById(mentorId)
      if (refreshed) onChanged(refreshed)
      setConfirming(false)
      setState('idle')
    } catch (err) {
      setState('error')
      setError(err instanceof ApiError ? err.message : 'Failed to change username')
    }
  }

  return (
    <div className="rounded-panel border border-line bg-surface/40 p-4">
      <label htmlFor="admin-username" className={labelClass}>
        Username (profile link)
      </label>
      <div className="flex items-center overflow-hidden rounded-field border-[1.5px] border-line bg-white focus-within:border-brand-cobalt">
        <span className="select-none whitespace-nowrap py-2 pl-3 pr-0 font-mono text-[13px] text-ink-soft">
          /mentor/
        </span>
        <input
          id="admin-username"
          value={value}
          maxLength={USERNAME_MAX_LENGTH}
          autoCapitalize="none"
          spellCheck={false}
          onChange={(e) => {
            setConfirming(false)
            setState('idle')
            setValue(
              e.target.value
                .toLowerCase()
                .replace(/[^a-z0-9-]+/g, '-')
                .replace(/-{2,}/g, '-')
            )
          }}
          className="w-full min-w-0 border-0 bg-transparent py-2 pl-0 pr-3 font-mono text-[13px] text-ink outline-none"
        />
      </div>

      <div className="mt-1.5 min-h-[1.125rem] text-[13px]" role="status" aria-live="polite">
        {isUnchanged && <span className="text-ink-soft">Current username.</span>}
        {!isUnchanged && availability.status === 'checking' && (
          <span className="text-ink-soft">
            <FontAwesomeIcon icon={faCircleNotch} spin className="mr-1" />
            Checking…
          </span>
        )}
        {!isUnchanged && availability.status === 'available' && (
          <span className="font-medium text-mint-ink">✓ Available</span>
        )}
        {!isUnchanged && availability.status === 'unavailable' && (
          <span className="font-medium text-danger">
            {availability.reason === 'taken'
              ? 'Already taken.'
              : availability.reason === 'reserved'
                ? 'Reserved word.'
                : 'Use 3–40 letters, numbers and hyphens.'}
          </span>
        )}
      </div>

      {error && (
        <p className="mt-1 text-[13px] font-medium text-danger" role="alert">
          {error}
        </p>
      )}

      {!confirming ? (
        <button
          type="button"
          disabled={!canSubmit}
          onClick={() => setConfirming(true)}
          className="button button-secondary mt-2 text-[13px] disabled:cursor-not-allowed disabled:opacity-50"
        >
          Change username…
        </button>
      ) : (
        <div className="mt-2 rounded-field border border-danger/40 bg-danger/[0.06] p-3">
          <p className="mb-2 text-[13px] text-ink">
            Rename to <span className="font-mono font-semibold">/mentor/{value}</span>? The old link
            <span className="font-mono"> /mentor/{currentUsername}</span> will redirect for now, but
            frees up after two more changes.
          </p>
          <div className="flex gap-2">
            <button
              type="button"
              disabled={state === 'loading'}
              onClick={onConfirm}
              className="button button-primary text-[13px]"
            >
              {state === 'loading' ? (
                <>
                  <FontAwesomeIcon icon={faCircleNotch} spin className="mr-1.5" />
                  Changing…
                </>
              ) : (
                'Yes, change it'
              )}
            </button>
            <button
              type="button"
              disabled={state === 'loading'}
              onClick={() => setConfirming(false)}
              className="button button-secondary text-[13px]"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function MentorModerationEditContent(): JSX.Element {
  const router = useRouter()
  const { id } = router.query
  const mentorId = Array.isArray(id) ? id[0] : id
  const { isAuthenticated, isLoading: authLoading, session } = useAdminAuth()

  const [mentor, setMentor] = useState<AdminMentorDetails | null>(null)
  const [formData, setFormData] = useState<AdminMentorProfileUpdateRequest | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saveState, setSaveState] = useState<SaveState>('idle')
  const [actionError, setActionError] = useState<string | null>(null)
  const [pictureState, setPictureState] = useState<PictureState>('idle')
  const [selectedImage, setSelectedImage] = useState<File | null>(null)
  const [imagePreview, setImagePreview] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Return-for-edits inline form
  const [showReturnForm, setShowReturnForm] = useState(false)
  const [returnReason, setReturnReason] = useState('')
  const [returnError, setReturnError] = useState<string | null>(null)
  const [isReturning, setIsReturning] = useState(false)

  // Decline confirm step
  const [confirmingDecline, setConfirmingDecline] = useState(false)
  const [isDeclining, setIsDeclining] = useState(false)

  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      router.replace('/admin/login')
    }
  }, [authLoading, isAuthenticated, router])

  useEffect(() => {
    if (!isAuthenticated || !session || !mentorId) return

    let mounted = true
    const loadMentor = async (): Promise<void> => {
      try {
        setIsLoading(true)
        setError(null)
        const data = await getModerationMentorById(mentorId)
        if (!data) {
          setError('Mentor not found')
          return
        }

        if (session.role === 'moderator' && data.status !== 'pending') {
          router.replace('/admin/mentors/pending')
          return
        }

        if (!mounted) return
        setMentor(data)
        setFormData(buildFormData(data))
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : 'Failed to load mentor')
        }
      } finally {
        if (mounted) {
          setIsLoading(false)
        }
      }
    }

    loadMentor()
    return () => {
      mounted = false
    }
  }, [isAuthenticated, session, mentorId, router])

  useEffect(() => {
    if (!session || session.role !== 'moderator' || !mentor) return
    if (mentor.status !== 'pending') {
      router.replace('/admin/mentors/pending')
    }
  }, [mentor, session, router])

  const availableTags = useMemo(() => {
    const selected = formData?.tags || []
    return Array.from(new Set([...filters.tags, ...selected])).sort((a, b) => a.localeCompare(b))
  }, [formData?.tags])

  const calendarUrlInvalid = !isValidCalendarUrl(formData?.calendarUrl.trim())

  const handleInputChange = (
    field: keyof AdminMentorProfileUpdateRequest,
    value: string | string[]
  ): void => {
    if (!formData) return
    setFormData({ ...formData, [field]: value })
    setSaveState('idle')
  }

  const toggleTag = (tag: string): void => {
    if (!formData) return
    const hasTag = formData.tags.includes(tag)
    const nextTags = hasTag ? formData.tags.filter((item) => item !== tag) : [...formData.tags, tag]
    setFormData({ ...formData, tags: nextTags })
    setSaveState('idle')
  }

  const onSave = async (): Promise<void> => {
    if (!mentor || !formData) return
    // The form is seeded from stored data, which predates the https_url
    // binding, so a legacy calendar link would fail the save as an opaque
    // "Invalid request body" naming no field.
    if (calendarUrlInvalid) {
      setSaveState('error')
      setActionError('Calendar URL must be an https:// link, or empty.')
      return
    }
    const payload = { ...formData, calendarUrl: formData.calendarUrl.trim() }
    setSaveState('loading')
    setActionError(null)
    try {
      const updated = await updateModerationMentor(mentor.mentorId, payload)
      setMentor(updated)
      setFormData(buildFormData(updated))
      setSaveState('success')
    } catch (err) {
      setSaveState('error')
      setActionError(err instanceof Error ? err.message : 'Failed to save changes')
    }
  }

  const onApprove = async (): Promise<void> => {
    if (!mentor) return
    setSaveState('idle')
    setActionError(null)
    setConfirmingDecline(false)
    try {
      const updated = await approveModerationMentor(mentor.mentorId)
      setMentor(updated)
      setShowReturnForm(false)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to approve mentor')
    }
  }

  const onDecline = async (): Promise<void> => {
    if (!mentor) return
    setSaveState('idle')
    setActionError(null)
    setIsDeclining(true)
    try {
      const updated = await declineModerationMentor(mentor.mentorId)
      setMentor(updated)
      setShowReturnForm(false)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to decline mentor')
    } finally {
      setIsDeclining(false)
      setConfirmingDecline(false)
    }
  }

  const onReturn = async (): Promise<void> => {
    if (!mentor || isReturning) return

    const reason = returnReason.trim()
    if (!reason) {
      setReturnError('A reason for the mentor is required.')
      return
    }
    if (reason.length > RETURN_REASON_MAX) {
      setReturnError(`The reason must be at most ${RETURN_REASON_MAX} characters.`)
      return
    }

    setIsReturning(true)
    setReturnError(null)
    setActionError(null)
    try {
      const updated = await returnModerationMentor(mentor.mentorId, reason)
      setMentor(updated)
      setShowReturnForm(false)
      setReturnReason('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setReturnError(
          'This mentor has already been activated and cannot be returned to draft. Edit the profile directly or decline it instead.'
        )
      } else {
        setReturnError(err instanceof Error ? err.message : 'Failed to return the profile')
      }
    } finally {
      setIsReturning(false)
    }
  }

  const onToggleActive = async (status: 'active' | 'inactive'): Promise<void> => {
    if (!mentor || session?.role !== 'admin') return
    setSaveState('idle')
    setActionError(null)
    try {
      const updated = await updateModerationMentorStatus(mentor.mentorId, { status })
      setMentor(updated)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to change status')
    }
  }

  const handleImageSelect = (event: ChangeEvent<HTMLInputElement>): void => {
    const file = event.target.files?.[0]
    if (!file) return

    const allowedTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp']
    if (!allowedTypes.includes(file.type)) {
      setActionError('Please select JPEG, PNG or WebP image')
      return
    }

    const maxSize = 10 * 1024 * 1024
    if (file.size > maxSize) {
      setActionError('Image size should not exceed 10MB')
      return
    }

    setSelectedImage(file)
    const reader = new FileReader()
    reader.onloadend = () => {
      setImagePreview(reader.result as string)
    }
    reader.readAsDataURL(file)
  }

  const onUploadPicture = async (): Promise<void> => {
    if (!mentor || !selectedImage || !imagePreview) return

    const payload: UploadProfilePictureRequest = {
      image: imagePreview,
      fileName: selectedImage.name,
      contentType: selectedImage.type,
    }

    setPictureState('loading')
    setSaveState('idle')
    setActionError(null)
    try {
      const result = await uploadModerationMentorPicture(mentor.mentorId, payload)
      if (!result.success) {
        throw new Error(result.message || 'Failed to upload image')
      }
      setPictureState('success')
      setSelectedImage(null)
      setImagePreview(null)
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
    } catch (err) {
      setPictureState('error')
      setActionError(err instanceof Error ? err.message : 'Failed to upload image')
    }
  }

  if (authLoading || !isAuthenticated) {
    return <AuthGateScreen title="Mentor moderation" />
  }

  return (
    <AdminLayout title="Mentor review">
      <NoIndexHead title="Mentor moderation" />

      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <FontAwesomeIcon
            icon={faCircleNotch}
            className="animate-spin text-2xl text-brand-cobalt"
          />
        </div>
      )}

      {!isLoading && error && (
        <div className="rounded-card border border-danger/40 bg-white p-4 text-sm font-medium text-danger">
          {error}
        </div>
      )}

      {!isLoading && !error && mentor && formData && (
        <div className="space-y-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-4">
              <Link
                href={getBackLink(mentor.status)}
                className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
              >
                ← Back to list
              </Link>
              <Link
                href={`/mentor/${mentor.slug}`}
                target="_blank"
                rel="noreferrer"
                className="text-sm font-semibold text-brand-cobalt transition-colors duration-120 hover:text-brand-navy"
              >
                Open mentor profile ↗
              </Link>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              {/* Requests the mentor received. Admin-only: moderators are
                  scoped to the pending-profile queue and the backend rejects
                  the endpoint for them. */}
              {session?.role === 'admin' && (
                <Link
                  href={`/admin/mentors/${encodeURIComponent(mentor.mentorId)}/requests`}
                  className="button-secondary text-[13px]"
                >
                  Requests ({mentor.requestsCount})
                </Link>
              )}
              <span className={moderationStatusBadgeClass(mentor.status)}>{mentor.status}</span>
            </div>
          </div>

          {/* Reviewer note on a returned (draft) profile */}
          {mentor.status === 'draft' && mentor.moderationNote && (
            <div className="rounded-panel border border-l-4 border-line border-l-danger bg-white p-5">
              <div className="font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                Returned for edits
              </div>
              <p className="my-0 mt-2 whitespace-pre-wrap text-sm leading-relaxed text-ink">
                {mentor.moderationNote}
              </p>
              <p className="my-0 mt-2 text-[13px] text-ink-soft">
                The mentor is editing their profile — it will come back to the pending queue once
                they resubmit.
              </p>
            </div>
          )}

          {/* Moderation actions: approve / return for edits / decline */}
          <div className="flex flex-wrap items-center gap-2.5">
            <button type="button" onClick={onApprove} className="button">
              Approve
            </button>

            {mentor.status === 'pending' && (
              <button
                type="button"
                onClick={() => {
                  setShowReturnForm((value) => !value)
                  setReturnError(null)
                  setConfirmingDecline(false)
                }}
                className="button-secondary"
                aria-expanded={showReturnForm}
              >
                Return for edits…
              </button>
            )}

            {confirmingDecline ? (
              <span className="inline-flex items-center gap-2">
                <button
                  type="button"
                  onClick={onDecline}
                  disabled={isDeclining}
                  className="button-destructive disabled:opacity-50"
                >
                  {isDeclining ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                      Declining...
                    </>
                  ) : (
                    'Confirm decline'
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmingDecline(false)}
                  disabled={isDeclining}
                  className="button-ghost"
                >
                  Cancel
                </button>
              </span>
            ) : (
              <button
                type="button"
                onClick={() => {
                  setConfirmingDecline(true)
                  setShowReturnForm(false)
                }}
                className="button-destructive"
              >
                Decline…
              </button>
            )}
          </div>

          {confirmingDecline && (
            <p className="my-0 text-sm text-ink-soft">
              Decline is a hard reject (spam / not a real profile). To ask for fixes, use
              &quot;Return for edits&quot; instead.
            </p>
          )}

          {/* Inline return-for-edits reason form */}
          {showReturnForm && (
            <div className="animate-dropdown-in rounded-panel border border-line bg-white p-5">
              <label htmlFor="return-reason" className={labelClass}>
                What should the mentor fix? <span className="text-danger">*</span>
              </label>
              <p className="my-0 mb-2 text-[13px] text-ink-soft">
                This note is emailed to the mentor and shown on their profile edit page. The
                profile goes back to draft until they resubmit.
              </p>
              <textarea
                id="return-reason"
                value={returnReason}
                onChange={(e) => {
                  setReturnReason(e.target.value)
                  setReturnError(null)
                }}
                rows={4}
                maxLength={RETURN_REASON_MAX}
                disabled={isReturning}
                placeholder="e.g. Please add a real photo and expand the about section…"
                className="field"
              />
              <div className="meta-mono mt-1 text-ink-mute">
                {returnReason.length} / {RETURN_REASON_MAX}
              </div>
              {returnError && (
                <p className="my-0 mt-2 text-sm font-medium text-danger" role="alert">
                  {returnError}
                </p>
              )}
              <div className="mt-3 flex flex-wrap gap-2.5">
                <button
                  type="button"
                  onClick={onReturn}
                  disabled={isReturning || !returnReason.trim()}
                  className="button disabled:opacity-50"
                >
                  {isReturning ? (
                    <>
                      <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                      Returning...
                    </>
                  ) : (
                    'Return to mentor'
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setShowReturnForm(false)
                    setReturnError(null)
                  }}
                  disabled={isReturning}
                  className="button-ghost"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}

          {session?.role === 'admin' && (
            <div className="flex flex-wrap gap-2.5">
              <button
                type="button"
                onClick={() => onToggleActive('active')}
                className="button-secondary"
              >
                Set active
              </button>
              <button
                type="button"
                onClick={() => onToggleActive('inactive')}
                className="button-secondary"
              >
                Set inactive
              </button>
            </div>
          )}

          {actionError && saveState !== 'error' && (
            <div className="rounded-card border border-danger/40 bg-white p-3 text-sm font-medium text-danger">
              {actionError}
            </div>
          )}

          <div className="grid gap-4 rounded-panel border border-line bg-white p-6 md:grid-cols-2">
            <div>
              <label className={labelClass}>Name</label>
              <input
                value={formData.name}
                onChange={(e) => handleInputChange('name', e.target.value)}
                className="field"
              />
            </div>
            <div>
              <label className={labelClass}>Email</label>
              <input
                value={formData.email}
                onChange={(e) => handleInputChange('email', e.target.value)}
                className="field"
              />
            </div>
            <div>
              <label className={labelClass}>Contact</label>
              <input
                value={formData.contact}
                onChange={(e) => handleInputChange('contact', e.target.value)}
                className="field"
              />
            </div>
            <div>
              <label className={labelClass}>Job</label>
              <input
                value={formData.job}
                onChange={(e) => handleInputChange('job', e.target.value)}
                className="field"
              />
            </div>
            <div>
              <label className={labelClass}>Workplace</label>
              <input
                value={formData.workplace}
                onChange={(e) => handleInputChange('workplace', e.target.value)}
                className="field"
              />
            </div>
            {session?.role === 'admin' && mentor && mentorId && (
              <AdminUsernameField
                mentorId={mentorId}
                currentUsername={mentor.slug}
                onChanged={(updated) => {
                  // Only refresh the mentor object (its slug drives this block's
                  // current-username display). Do NOT rebuild formData — that
                  // would discard any unsaved profile edits the admin made
                  // before renaming. A rename never touches profile fields.
                  setMentor(updated)
                }}
              />
            )}
            <div>
              <label className={labelClass}>Experience</label>
              <select
                value={formData.experience}
                onChange={(e) => handleInputChange('experience', e.target.value)}
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
              <label className={labelClass}>Price</label>
              <select
                value={formData.price}
                onChange={(e) => handleInputChange('price', e.target.value)}
                className="field"
              >
                <option value="">Select price</option>
                {filters.price.map((item) => (
                  <option key={item} value={item}>
                    {item}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className={labelClass}>Calendar URL</label>
              <input
                value={formData.calendarUrl}
                onChange={(e) => handleInputChange('calendarUrl', e.target.value)}
                className={`field ${calendarUrlInvalid ? 'field-error' : ''}`}
              />
              {calendarUrlInvalid && (
                <p className="mt-1 text-sm font-medium text-danger" role="alert">
                  Must be an https:// link, or empty. Saving is blocked until this is fixed.
                </p>
              )}
            </div>

            <div className="md:col-span-2">
              <p className={labelClass}>Tags</p>
              <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                {availableTags.map((tag) => {
                  const selected = formData.tags.includes(tag)
                  return (
                    <label
                      key={tag}
                      className={`flex cursor-pointer items-center gap-2 rounded-field border px-3 py-2 text-sm text-ink ${
                        selected
                          ? 'border-brand-cobalt bg-brand-cobalt/5'
                          : 'border-line bg-white'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={selected}
                        onChange={() => toggleTag(tag)}
                        className="h-4 w-4 rounded border-line text-brand-cobalt focus:ring-brand-cobalt"
                      />
                      <span>{tag}</span>
                    </label>
                  )
                })}
              </div>
            </div>

            <div className="md:col-span-2">
              <label className={labelClass}>About</label>
              <Wysiwyg
                key={`about-${mentor.updatedAt}`}
                content={formData.about}
                onUpdate={(editor) => handleInputChange('about', editor.getHTML())}
              />
            </div>
            <div className="md:col-span-2">
              <label className={labelClass}>Description</label>
              <Wysiwyg
                key={`description-${mentor.updatedAt}`}
                content={formData.description}
                onUpdate={(editor) => handleInputChange('description', editor.getHTML())}
              />
            </div>
            <div className="md:col-span-2">
              <label className={labelClass}>Competencies</label>
              <textarea
                value={formData.competencies}
                onChange={(e) => handleInputChange('competencies', e.target.value)}
                className="field h-20"
              />
            </div>
          </div>

          {session?.role === 'admin' && (
            <div className="rounded-panel border border-line bg-white p-6">
              <h3 className="mb-3 font-display text-[13px] font-extrabold uppercase tracking-[0.03em] text-ink">
                Profile picture
              </h3>
              <div className="mb-3 flex items-center gap-4">
                <Image
                  src={imagePreview || imageLoader({ src: mentor.mentorId, quality: 'full' })}
                  alt="Mentor picture"
                  width={96}
                  height={96}
                  className="h-24 w-24 rounded-full object-cover"
                  unoptimized
                />
                <div className="flex-1">
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/jpeg,image/jpg,image/png,image/webp"
                    onChange={handleImageSelect}
                    className="block w-full text-sm text-ink-soft file:mr-4 file:rounded-field file:border-0 file:bg-brand-cobalt/10 file:px-4 file:py-2 file:text-sm file:font-medium file:text-brand-cobalt hover:file:bg-brand-cobalt/20"
                  />
                  {pictureState === 'success' && (
                    <p className="my-0 mt-2 text-sm font-medium text-mint-ink">
                      Picture uploaded successfully.
                    </p>
                  )}
                  {pictureState === 'error' && (
                    <p className="my-0 mt-2 text-sm font-medium text-danger">
                      Picture upload failed.
                    </p>
                  )}
                </div>
              </div>
              <button
                type="button"
                onClick={onUploadPicture}
                disabled={pictureState === 'loading' || !selectedImage}
                className="button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {pictureState === 'loading' ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                    Uploading...
                  </>
                ) : (
                  'Upload picture'
                )}
              </button>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              onClick={onSave}
              disabled={saveState === 'loading'}
              className="button disabled:cursor-not-allowed disabled:opacity-50"
            >
              {saveState === 'loading' ? (
                <>
                  <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                'Save changes'
              )}
            </button>
            {saveState === 'success' && (
              <span className="text-sm font-medium text-mint-ink">Mentor saved successfully.</span>
            )}
            {saveState === 'error' && (
              <span className="text-sm font-medium text-danger">
                {actionError || 'Failed to save changes.'}
              </span>
            )}
          </div>
        </div>
      )}
    </AdminLayout>
  )
}

export default function MentorModerationEditPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <MentorModerationEditContent />
    </AdminAuthProvider>
  )
}
