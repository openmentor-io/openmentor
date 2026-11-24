/**
 * Mentee dashboard types (D95). Mirrors api/internal/models/mentee.go.
 */

import type { RequestStatus } from './mentor-requests'

/** Mentee session. Carries no email, like the mentor session. */
export interface MenteeSession {
  mentee_id: string
  exp: number
  iat: number
}

/** The dashboard bucket a request is shown in, decided by the API. */
export type MenteeRequestGroup = 'pending' | 'active' | 'done' | 'closed'

/**
 * The only mentor information a mentee is shown. No contact details:
 * `calendarUrl` is present only for pending and active requests, `slug` only
 * while the public profile exists.
 */
export interface MenteeRequestMentor {
  name: string
  slug?: string
  calendarUrl?: string
  available: boolean
}

export interface MenteeReview {
  mentorReview: string
  platformReview: string
  improvements: string
  createdAt: string
}

export interface MenteeRequest {
  id: string
  status: RequestStatus
  group: MenteeRequestGroup
  details: string
  createdAt: string
  statusChangedAt: string | null
  declineReason?: string
  declineComment?: string
  mentor: MenteeRequestMentor
  review: MenteeReview | null
  canReview: boolean
}

export interface MenteeRequestsResponse {
  requests: MenteeRequest[]
}

export interface MenteeSubmitReviewPayload {
  requestId: string
  mentorReview: string
  platformReview: string
  improvements: string
  captchaToken: string
}
