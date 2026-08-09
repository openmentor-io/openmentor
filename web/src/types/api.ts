/**
 * API request/response types for Go API client
 */

import type { NextApiRequest, NextApiResponse } from 'next'

/**
 * Parameters for fetching all mentors
 */
export interface GetAllMentorsParams {
  onlyVisible?: boolean
  drop_long_fields?: boolean
}

/**
 * Parameters for fetching a single mentor
 */
export interface GetOneMentorParams {
  showHiddenFields?: boolean
}

/**
 * Contact mentor form request
 */
export interface ContactMentorRequest {
  mentorId: string
  name: string
  email: string
  contact: string
  experience?: string
  intro: string
  captchaToken: string
}

/**
 * Contact mentor response
 */
export interface ContactMentorResponse {
  success: boolean
  requestId?: string
  calendar_url?: string
  error?: string
  /**
   * Machine-readable rejection code. 'mentor_not_contactable' (HTTP 409) means the
   * mentor is not accepting requests: the API gates the insert in SQL, so no
   * request was created and no calendar link comes back. The page renders `error`;
   * this field is for anything that needs to branch on the cause.
   */
  reason?: string
}

/**
 * Schedule a getmentor.dev profile migration (public /migrate page)
 */
export interface ScheduleMigrationRequest {
  slug: string
  captchaToken: string
}

/**
 * Schedule migration response
 */
export interface ScheduleMigrationResponse {
  success: boolean
  alreadyScheduled?: boolean
  error?: string
}

/**
 * Confirm a mentor's email address (public /mentor/confirm page,
 * draft-status registration flow). The same payload is used by the
 * confirmation resend endpoint.
 */
export interface ConfirmMentorEmailRequest {
  token: string
}

/**
 * Confirm mentor email response. `code` distinguishes an expired link
 * (offer a resend) from a dead one.
 */
export interface ConfirmMentorEmailResponse {
  success: boolean
  already?: boolean
  error?: string
  code?: 'invalid_token' | 'token_expired'
}

/**
 * Save profile request
 */
export interface SaveProfileRequest {
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
 * Save profile response
 */
export interface SaveProfileResponse {
  success: boolean
  message?: string
}

/**
 * Upload profile picture request
 */
export interface UploadProfilePictureRequest {
  image: string // base64 encoded
  fileName: string
  contentType: string
}

/**
 * Upload profile picture response
 */
export interface UploadProfilePictureResponse {
  success: boolean
  imageUrl?: string
  message?: string
}

/**
 * Update profile visibility status request (mentor self-serve)
 */
export interface UpdateProfileStatusRequest {
  status: 'active' | 'inactive'
}

/**
 * Update profile visibility status response
 */
export interface UpdateProfileStatusResponse {
  success: boolean
  status?: 'active' | 'inactive'
  error?: string
}

// ============================================================
// Draft-workflow types (mentor submit-for-review + admin return)
// ============================================================

/**
 * Submit a draft profile for review response
 * (POST /api/mentor/profile/submit — mentor self-serve, session auth)
 */
export interface SubmitProfileResponse {
  success: boolean
  status?: string
  error?: string
}

/**
 * Return a pending profile to draft with a required reviewer note
 * (POST /api/admin/mentors/:id/return)
 */
export interface AdminMentorReturnRequest {
  reason: string
}

// ============================================================
// Profile deletion (D70)
// ============================================================

/**
 * Mentor deleting their own profile
 * (POST /api/mentor/profile/delete — session auth).
 *
 * `username` is the value retyped into the confirm dialog. The API checks it
 * against the session's own profile, so it confirms intent; it never selects
 * which profile is deleted.
 */
export interface DeleteProfileRequest {
  username: string
}

export interface DeleteProfileResponse {
  success: boolean
  error?: string
}

/**
 * Admin deleting a mentor's profile
 * (POST /api/admin/mentors/:id/delete). `username` is the TARGET profile's
 * username, retyped by the admin.
 */
export interface AdminMentorDeleteRequest {
  username: string
}

// ============================================================
// End of draft-workflow types
// ============================================================

/**
 * Profile picture data for registration
 */
export interface ProfilePictureData {
  image: string // base64 encoded
  fileName: string
  contentType: string // 'image/jpeg' | 'image/png' | 'image/webp'
}

/**
 * Register mentor request
 */
export interface RegisterMentorRequest {
  name: string
  email: string
  contact: string
  /** Chosen profile URL part (public name for the internal slug). Optional — auto-generated when empty. */
  username?: string
  job: string
  workplace: string
  experience: string // '2-5' | '5-10' | '10+'
  price: string
  tags: string[]
  about: string
  description: string
  competencies: string
  calendarUrl?: string
  profilePicture: ProfilePictureData
  captchaToken: string
}

/**
 * Register mentor response
 */
export interface RegisterMentorResponse {
  success: boolean
  message?: string
  mentorId?: number
  error?: string
  /** Machine-readable error code, e.g. 'username_taken' | 'username_invalid' */
  reason?: string
}

/**
 * Username (public name for the mentor slug) API types
 */
export interface UsernameAvailabilityResult {
  username: string
  available: boolean
  reason?: 'taken' | 'invalid' | 'reserved'
  message?: string
}

export interface UsernameStatusResponse {
  username: string
  canChange: boolean
  nextChangeAt?: string // ISO timestamp, present when cooldown is active
}

export interface ChangeUsernameResponse {
  success: boolean
  username: string
}

/**
 * HTTP Error class with status code
 */
export class HttpError extends Error {
  statusCode: number
  statusText: string
  body: string

  constructor(statusCode: number, statusText: string, body: string) {
    super(`Go API error: ${statusCode} ${statusText} - ${body}`)
    this.name = 'HttpError'
    this.statusCode = statusCode
    this.statusText = statusText
    this.body = body
  }
}

/**
 * API route handler type
 */
export type ApiHandler<T = unknown> = (
  req: NextApiRequest,
  res: NextApiResponse<T>
) => Promise<void> | void

/**
 * Standard API error response
 */
export interface ApiErrorResponse {
  error: string
  message?: string
}

/**
 * Go API internal request body
 */
export interface GoApiInternalRequest {
  only_visible?: boolean
  drop_long_fields?: boolean
  show_hidden?: boolean
}

/**
 * Request options for Go API client
 */
export interface GoApiRequestOptions {
  headers?: Record<string, string>
  body?: Record<string, unknown>
}
