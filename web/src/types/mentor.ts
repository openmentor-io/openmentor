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
 * The mentor tag taxonomy (DECISIONS D30).
 *
 * This union is the canonical list. `src/config/filters.ts` types its group
 * constants as `MentorTag[]`, so adding a tag there without adding it here
 * (or vice versa) fails the type check — that drift is what left `Security`
 * and `Other` offered in the UI but absent from the database.
 *
 * Must also match api/migrations/000009_modernise_tags.up.sql, which the DB
 * unique constraint on tags.name enforces at runtime.
 */
export type MentorTag =
  // Engineering
  | 'Backend'
  | 'Frontend'
  | 'Mobile'
  | 'System Design'
  | 'Security'
  | 'QA & Test Automation'
  // AI & Data
  | 'AI/LLM Engineering'
  | 'Machine Learning'
  | 'Data Engineering'
  | 'Data & Analytics'
  // Infrastructure
  | 'DevOps/SRE'
  | 'Platform Engineering'
  | 'Cloud & Infrastructure'
  | 'Databases'
  // Product & Design
  | 'Product Management'
  | 'UX/UI Design'
  // Leadership & Career
  | 'Engineering Management'
  | 'Tech Lead'
  | 'Staff+/IC Growth'
  | 'Career Growth'
  | 'Interview Prep'
  | 'Compensation & Negotiation'
  | 'Relocation & Working Abroad'
  | 'Project Management'
  // Business & Communication
  | 'Entrepreneurship & Startups'
  | 'Freelancing & Consulting'
  | 'Developer Relations'
  | 'Technical Writing'
  | 'Marketing & Growth'
  | 'People & HR'

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
   * Number of completed mentorship sessions, including any carried over from
   * getmentor.dev at migration time. Optional: only present in newer Go API
   * payloads — the UI must work when it's absent.
   */
  sessionsCount?: number
  /**
   * The share of sessionsCount that happened on getmentor.dev before the
   * profile was migrated (DECISIONS D28). 0 for mentors who registered on
   * OpenMentor; optional for the same payload-age reason as sessionsCount.
   */
  legacySessionsCount?: number
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
 * Exactly the fields a catalog card renders.
 *
 * `MentorsList` takes this rather than a full `MentorListItem` so a page can
 * project before returning props: everything handed to props is serialized
 * into `__NEXT_DATA__`, and `drop_long_fields` clears only `about`/
 * `description` — `competencies` alone accepts up to 5,000 characters and no
 * card reads it. A full `MentorListItem` still satisfies this shape, so
 * callers that already have one (the homepage) need no change.
 */
export type MentorCardItem = Pick<
  MentorListItem,
  | 'id'
  | 'mentorId'
  | 'slug'
  | 'name'
  | 'job'
  | 'workplace'
  | 'experience'
  | 'price'
  | 'sessionsCount'
  | 'isNew'
  | 'photoStyle'
  | 'updatedAt'
>

/**
 * Type guard for mentor with secure fields
 */
export function hasMentorSecureFields(
  mentor: MentorBase | MentorWithSecureFields
): mentor is MentorWithSecureFields {
  return 'calendarUrl' in mentor
}
