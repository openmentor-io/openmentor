import type { RequestStatus } from './mentor-requests'

export type ModeratorRole = 'moderator' | 'admin'

export type MentorModerationFilter = 'pending' | 'approved' | 'declined'

// 'draft' — returned for edits (moderationNote) or awaiting email confirmation
export type MentorModerationStatus = 'draft' | 'pending' | 'active' | 'inactive' | 'declined'

export interface AdminSession {
  moderatorId: string
  email: string
  name: string
  role: ModeratorRole
  exp: number
  iat: number
}

export interface AdminMentorListItem {
  mentorId: string
  id: number
  name: string
  email: string
  contact: string
  job: string
  workplace: string
  price: string
  status: MentorModerationStatus
  createdAt: string
}

export interface AdminMentorDetails {
  mentorId: string
  id: number
  slug: string
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
  calendarUrl: string
  status: MentorModerationStatus
  sortOrder: number
  /** Reviewer note written when the profile was returned to draft (cleared on approve). */
  moderationNote?: string
  /** Auto-detected profile picture display style. */
  photoStyle?: string
  /** Set on first approve; once set the mentor can never be returned to draft. */
  activatedAt?: string | null
  /** Total mentee requests received (all statuses) — drives the "Requests (N)" button. */
  requestsCount: number
  createdAt: string
  updatedAt: string
}

export interface AdminMentorsListResponse {
  mentors: AdminMentorListItem[]
  total: number
}

export interface AdminMentorResponse {
  mentor: AdminMentorDetails
}

export interface AdminMentorProfileUpdateRequest {
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
  calendarUrl: string
}

export interface AdminStatusUpdateRequest {
  status: 'active' | 'inactive'
}

/**
 * Admin override of a mentee request's status. Unlike the mentor-facing flow
 * this accepts ANY status, including moves out of a terminal one.
 */
export interface AdminRequestStatusUpdateRequest {
  status: RequestStatus
}

/** Status filter on the admin requests list ('all' = no server-side filter). */
export type AdminRequestStatusFilter = 'all' | RequestStatus
