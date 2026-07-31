/**
 * Types for Mentor Admin - Client Requests
 */

/**
 * Request status as defined in the spec
 */
export type RequestStatus =
  | 'pending'
  | 'contacted'
  | 'working'
  | 'done'
  | 'reschedule'
  | 'declined'
  | 'unavailable'

/**
 * Every valid status, in the order of the client_requests_status_chk CHECK
 * constraint (api/migrations/000001). The admin override (moderation portal)
 * can set any of these on any request; the mentor-facing flow is limited to
 * STATUS_TRANSITIONS.
 *
 * 'reschedule' is a getmentor.dev-era status that the database accepts but no
 * write path produces. It is listed here — and given a label and colors below
 * — so that an existing row carrying it renders in the admin panel instead of
 * crashing StatusBadge on an undefined STATUS_COLORS entry. It stays out of
 * ACTIVE_STATUSES/PAST_STATUSES: the mentor's own inbox has never shown it.
 */
export const ALL_REQUEST_STATUSES: RequestStatus[] = [
  'pending',
  'contacted',
  'working',
  'done',
  'reschedule',
  'declined',
  'unavailable',
]

/**
 * Active statuses - displayed on /mentor page
 */
export const ACTIVE_STATUSES: RequestStatus[] = ['pending', 'contacted', 'working']

/**
 * Past statuses - displayed on /mentor/past page
 */
export const PAST_STATUSES: RequestStatus[] = ['done', 'declined', 'unavailable']

/**
 * Status workflow - valid transitions
 * - pending → contacted → working → done
 * - declined allowed from any state except done
 */
export const STATUS_TRANSITIONS: Record<RequestStatus, RequestStatus[]> = {
  pending: ['contacted', 'declined'],
  contacted: ['working', 'declined'],
  working: ['done', 'declined'],
  done: [], // terminal state
  // No mentor-side transitions: mirrors CanTransitionTo in the Go model,
  // which falls through to false for 'reschedule'. Offering a button here
  // would just produce a 400. Only the admin override can move it.
  reschedule: [],
  declined: [], // terminal state
  unavailable: [], // terminal state
}

/**
 * Status display labels
 */
export const STATUS_LABELS: Record<RequestStatus, string> = {
  pending: 'Pending',
  contacted: 'Contacted',
  working: 'In progress',
  done: 'Completed',
  reschedule: 'Reschedule',
  declined: 'Declined',
  unavailable: 'Unavailable',
}

/**
 * Status badge colors (Tailwind classes) — redesign pill system
 * (component sheet + design 07): pending = solid cobalt, in progress =
 * mint tint, closed states = deep paper.
 */
export const STATUS_COLORS: Record<RequestStatus, { bg: string; text: string }> = {
  pending: { bg: 'bg-brand-cobalt', text: 'text-white' },
  contacted: { bg: 'bg-pastel-sky', text: 'text-brand-navy' },
  working: { bg: 'bg-brand-mint/15', text: 'text-mint-ink' },
  done: { bg: 'bg-surface-deep', text: 'text-brand-navy' },
  // Sand pastel — an attention state, distinct from the sky/mint in-flight
  // tints and from the deep-paper closed states.
  reschedule: { bg: 'bg-pastel-sand', text: 'text-brand-navy' },
  declined: { bg: 'bg-surface-deep', text: 'text-ink-mute' },
  unavailable: { bg: 'bg-surface-deep', text: 'text-ink-mute' },
}

/**
 * Predefined decline reasons
 */
export const DECLINE_REASONS = [
  { value: 'no_time', label: 'Not enough time right now' },
  { value: 'topic_mismatch', label: 'The topic is outside my expertise' },
  { value: 'helping_others', label: 'Already working with other mentees' },
  { value: 'on_break', label: 'Temporarily not taking new requests' },
  { value: 'other', label: 'Other reason' },
] as const

export type DeclineReasonValue = (typeof DECLINE_REASONS)[number]['value']

/**
 * Client request from a mentee
 */
export interface MentorClientRequest {
  id: string
  email: string
  name: string
  contact: string
  details: string
  level: string
  createdAt: string // ISO date string
  modifiedAt: string // ISO date string
  statusChangedAt: string // ISO date string
  scheduledAt: string | null // ISO date string or null
  review: string | null
  status: RequestStatus
  mentorId: string
  reviewUrl: string | null
  declineReason: string | null
  declineComment: string | null
}

/**
 * Decline request payload
 */
export interface DeclineRequestPayload {
  reason: DeclineReasonValue
  comment?: string
}

/**
 * Update status payload
 */
export interface UpdateStatusPayload {
  status: RequestStatus
}

/**
 * API response for requests list
 */
export interface RequestsListResponse {
  requests: MentorClientRequest[]
  total: number
}

/**
 * Mentor session data (from auth)
 * Matches JWT payload from backend
 */
export interface MentorSession {
  mentor_uuid: string // NEW: Primary identifier (UUID from PostgreSQL)
  legacy_id: number // NEW: Old integer ID
  email: string
  name: string
  exp: number // Unix timestamp
  iat: number // Unix timestamp
}

/**
 * Auth request payload
 */
export interface RequestLoginPayload {
  email: string
}

/**
 * Auth verify payload
 */
export interface VerifyLoginPayload {
  token: string
}

/**
 * Auth response
 */
export interface AuthResponse {
  success: boolean
  message?: string
  session?: MentorSession
}

/**
 * Sort order for requests list
 */
export type SortOrder = 'newest' | 'oldest'
