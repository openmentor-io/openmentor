/**
 * Browser-side client for the mentee dashboard (D95). Calls the BFF routes under
 * /api/mentee; the session is an HttpOnly cookie the browser never reads.
 */

import type { MenteeSession, MenteeRequestsResponse, MenteeSubmitReviewPayload } from '@/types'
import { ApiError, apiRequest } from '@/lib/mentor-admin-api'

export { ApiError }

export async function getMenteeSession(): Promise<MenteeSession | null> {
  try {
    const response = await apiRequest<{ success: boolean; session?: MenteeSession }>(
      '/api/mentee/auth/session',
    )
    return response.success && response.session ? response.session : null
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      return null
    }
    throw error
  }
}

export async function requestMenteeLogin(
  email: string,
): Promise<{ success: boolean; message?: string }> {
  return apiRequest('/api/mentee/auth/request-login', {
    method: 'POST',
    body: JSON.stringify({ email }),
  })
}

export async function verifyMenteeLogin(
  token: string,
): Promise<{ success: boolean; session?: MenteeSession; message?: string }> {
  try {
    return await apiRequest('/api/mentee/auth/verify', {
      method: 'POST',
      body: JSON.stringify({ token }),
    })
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      return { success: false, message: 'The link is invalid or has expired' }
    }
    throw error
  }
}

export async function logoutMentee(): Promise<void> {
  await apiRequest('/api/mentee/auth/logout', { method: 'POST' })
}

export async function getMenteeRequests(): Promise<MenteeRequestsResponse> {
  return apiRequest<MenteeRequestsResponse>('/api/mentee/requests')
}

export async function submitMenteeReview(
  payload: MenteeSubmitReviewPayload,
): Promise<{ success: boolean; reviewId?: string; error?: string }> {
  return apiRequest('/api/mentee/reviews', { method: 'POST', body: JSON.stringify(payload) })
}
