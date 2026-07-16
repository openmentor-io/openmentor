/**
 * Mentor domain types
 */

/**
 * Calendar integration types
 */
export type CalendarType = 'calendly' | 'koalendar' | 'calendlab' | 'url' | 'none'

/**
 * Experience level types
 */
export type ExperienceLevel = '2-5' | '5-10' | '10+'

/**
 * Price is free text (DECISIONS D3), e.g. "$100 / hour", "Free", "Negotiable"
 */
export type Price = string

/**
 * Mentor tag categories
 */
export type MentorTag =
  | 'Backend'
  | 'Frontend'
  | 'Code Review'
  | 'System Design'
  | 'UX/UI/Design'
  | 'iOS'
  | 'Android'
  | 'QA'
  | 'Marketing'
  | 'Content/Copy'
  | 'Databases'
  | 'Data Science/ML'
  | 'Analytics'
  | 'Networking'
  | 'Cloud'
  | 'Security'
  | 'DevOps/SRE'
  | 'Agile'
  | 'Team Lead/Management'
  | 'Project Management'
  | 'Product Management'
  | 'Entrepreneurship'
  | 'DevRel'
  | 'HR'
  | 'Career'
  | 'Interview prep'
  | 'Other'

/**
 * Combined tag type
 */
export type Tag = MentorTag

/**
 * Mentor profile lifecycle status.
 * Only 'active' profiles are visible in the public catalog.
 * 'draft' = submitted but not email-confirmed, or returned by a moderator
 * for edits (see moderationNote); confirming/resubmitting moves it to
 * 'pending'. Once 'active', a profile can never return to 'draft'.
 */
export type MentorProfileStatus = 'draft' | 'pending' | 'active' | 'inactive' | 'declined'

/**
 * Catalog card photo treatment, classified at upload time by the API
 * (border-luminance heuristic): 'hero' = light plain background, safe for
 * the multiply-blend cut-out look; 'frame' = arch-masked tile fallback.
 */
export type MentorPhotoStyle = 'hero' | 'frame'

/**
 * Base mentor data (public fields)
 */
export interface MentorBase {
  id: number
  mentorId: string
  slug: string
  name: string
  job: string
  workplace: string
  description: string | null
  about: string | null
  competencies: string
  experience: ExperienceLevel | string
  price: Price | string
  tags: string[]
  menteeCount: number
  /**
   * Number of completed mentorship sessions. Optional: only present in
   * newer Go API payloads — the UI must work when it's absent.
   */
  sessionsCount?: number
  photo_url: string | null
  sortOrder: number
  isVisible: boolean
  isNew: boolean
  calendarType: CalendarType
  updatedAt?: string
  status?: MentorProfileStatus
  /**
   * Card photo treatment (see MentorPhotoStyle). Optional: absent in older
   * payloads and for mentors without a photo — treat as 'frame'.
   */
  photoStyle?: MentorPhotoStyle
  /**
   * Reviewer note attached when a moderator returns the profile to 'draft'
   * for edits. Only present on authenticated own-profile payloads.
   */
  moderationNote?: string | null
}

/**
 * Mentor with hidden/secure fields (for authenticated access)
 */
export interface MentorWithSecureFields extends MentorBase {
  calendarUrl: string | null
}

/**
 * Mentor type for list view (with potentially dropped long fields)
 */
export interface MentorListItem extends Omit<MentorBase, 'description' | 'about'> {
  description?: string | null
  about?: string | null
}

/**
 * Type guard for mentor with secure fields
 */
export function hasMentorSecureFields(
  mentor: MentorBase | MentorWithSecureFields
): mentor is MentorWithSecureFields {
  return 'calendarUrl' in mentor
}
