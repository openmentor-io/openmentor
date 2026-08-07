import type {
  AdminSession,
  MentorModerationFilter,
  AdminMentorListItem,
  AdminMentorDetails,
  AdminMentorProfileUpdateRequest,
  AdminStatusUpdateRequest,
  UploadProfilePictureRequest,
  UploadProfilePictureResponse,
  ChangeUsernameResponse,
  MentorClientRequest,
  RequestsListResponse,
  RequestStatus,
} from '@/types'

export class ApiError extends Error {
  constructor(message: string, public status: number, public details?: unknown) {
    super(message)
    this.name = 'ApiError'
  }
}

async function apiRequest<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(endpoint, {
    ...options,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  })

  if (!response.ok) {
    let errorMessage = 'Something went wrong'
    let errorDetails: unknown

    try {
      const errorData = await response.json()
      errorMessage = errorData.error || errorData.message || errorMessage
      errorDetails = errorData.details
    } catch {
      // ignore parse errors
    }

    throw new ApiError(errorMessage, response.status, errorDetails)
  }

  const contentType = response.headers.get('content-type')
  if (!contentType || !contentType.includes('application/json')) {
    return {} as T
  }

  return response.json()
}

let cachedSession: AdminSession | null = null

export async function getAdminSession(): Promise<AdminSession | null> {
  if (cachedSession && cachedSession.exp * 1000 > Date.now()) {
    return cachedSession
  }

  try {
    const response = await apiRequest<{ success: boolean; session?: AdminSession }>(
      '/api/admin/auth/session'
    )
    if (response.success && response.session) {
      cachedSession = response.session
      return response.session
    }
    return null
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      cachedSession = null
      return null
    }
    throw error
  }
}

export function clearAdminSession(): void {
  cachedSession = null
}

export async function requestAdminLogin(
  email: string
): Promise<{ success: boolean; message?: string }> {
  const genericSuccessMessage =
    'If your email is registered in the system, you will receive a login link'

  try {
    await apiRequest<{ success: boolean; message?: string }>('/api/admin/auth/request-login', {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
    return { success: true, message: genericSuccessMessage }
  } catch (error) {
    if (error instanceof TypeError && error.message.includes('fetch')) {
      return {
        success: false,
        message: 'Could not connect to the server. Please check your connection.',
      }
    }
    return { success: true, message: genericSuccessMessage }
  }
}

export async function verifyAdminLogin(
  token: string
): Promise<{ success: boolean; session?: AdminSession; message?: string }> {
  try {
    const response = await apiRequest<{ success: boolean; session?: AdminSession; error?: string }>(
      '/api/admin/auth/verify',
      {
        method: 'POST',
        body: JSON.stringify({ token }),
      }
    )

    if (response.success && response.session) {
      cachedSession = response.session
      return { success: true, session: response.session }
    }

    return { success: false, message: response.error || 'Login failed' }
  } catch (error) {
    if (error instanceof ApiError) {
      return { success: false, message: error.message }
    }
    return { success: false, message: 'Invalid or expired token' }
  }
}

export async function logoutAdmin(): Promise<void> {
  try {
    await apiRequest<{ success: boolean }>('/api/admin/auth/logout', { method: 'POST' })
  } finally {
    cachedSession = null
  }
}

export async function getModerationMentors(
  status: MentorModerationFilter
): Promise<AdminMentorListItem[]> {
  const response = await apiRequest<{ mentors: AdminMentorListItem[]; total: number }>(
    `/api/admin/mentors?status=${status}`
  )
  return response.mentors
}

export async function getModerationMentorById(
  mentorId: string
): Promise<AdminMentorDetails | null> {
  try {
    const response = await apiRequest<{ mentor: AdminMentorDetails }>(
      `/api/admin/mentors/${encodeURIComponent(mentorId)}`
    )
    return response.mentor
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return null
    }
    throw error
  }
}

export async function updateModerationMentor(
  mentorId: string,
  payload: AdminMentorProfileUpdateRequest
): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}`,
    {
      method: 'POST',
      body: JSON.stringify(payload),
    }
  )
  return response.mentor
}

export async function approveModerationMentor(mentorId: string): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/approve`,
    { method: 'POST' }
  )
  return response.mentor
}

export async function declineModerationMentor(mentorId: string): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/decline`,
    { method: 'POST' }
  )
  return response.mentor
}

/**
 * Return a pending mentor profile to draft with a required reviewer note.
 * Throws ApiError with status 409 when the mentor has ever been activated.
 */
export async function returnModerationMentor(
  mentorId: string,
  reason: string
): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/return`,
    {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }
  )
  return response.mentor
}

/**
 * Delete a mentor's profile (D70). `username` is the target profile's own
 * username, retyped by the admin — the server re-checks it, so a mismatch
 * comes back as a 400 rather than deleting the wrong profile.
 */
export async function deleteModerationMentor(
  mentorId: string,
  username: string
): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/delete`,
    {
      method: 'POST',
      body: JSON.stringify({ username }),
    }
  )
  return response.mentor
}

/** Restore a deleted profile — comes back as 'inactive'. */
export async function restoreModerationMentor(mentorId: string): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/restore`,
    { method: 'POST' }
  )
  return response.mentor
}

export async function updateModerationMentorStatus(
  mentorId: string,
  payload: AdminStatusUpdateRequest
): Promise<AdminMentorDetails> {
  const response = await apiRequest<{ mentor: AdminMentorDetails }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/status`,
    {
      method: 'POST',
      body: JSON.stringify(payload),
    }
  )
  return response.mentor
}

export async function uploadModerationMentorPicture(
  mentorId: string,
  imageData: UploadProfilePictureRequest
): Promise<UploadProfilePictureResponse> {
  return apiRequest<UploadProfilePictureResponse>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/picture`,
    {
      method: 'POST',
      body: JSON.stringify(imageData),
    }
  )
}

/**
 * Change a mentor's username from the admin panel (no cooldown). Goes through
 * the same history/redirect machinery as the mentor flow. Throws ApiError with
 * status 409 when the username is already taken.
 */
export async function changeModerationMentorUsername(
  mentorId: string,
  username: string
): Promise<ChangeUsernameResponse> {
  return apiRequest<ChangeUsernameResponse>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/username`,
    {
      method: 'POST',
      body: JSON.stringify({ username }),
    }
  )
}

/**
 * List the requests a mentor received. Omitting `status` returns every status;
 * the list page filters client-side from there.
 */
export async function getModerationMentorRequests(
  mentorId: string,
  status?: RequestStatus
): Promise<MentorClientRequest[]> {
  const query = status ? `?status=${encodeURIComponent(status)}` : ''
  const response = await apiRequest<RequestsListResponse>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/requests${query}`
  )
  return response.requests
}

export async function getModerationMentorRequest(
  mentorId: string,
  requestId: string
): Promise<MentorClientRequest | null> {
  try {
    const response = await apiRequest<{ request: MentorClientRequest }>(
      `/api/admin/mentors/${encodeURIComponent(mentorId)}/requests/${encodeURIComponent(requestId)}`
    )
    return response.request
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return null
    }
    throw error
  }
}

/**
 * Override a request's status. Admins may set any status, including moving a
 * request back out of a terminal one — that restriction only applies to the
 * mentor's own inbox.
 */
export async function updateModerationRequestStatus(
  mentorId: string,
  requestId: string,
  status: RequestStatus
): Promise<MentorClientRequest> {
  const response = await apiRequest<{ request: MentorClientRequest }>(
    `/api/admin/mentors/${encodeURIComponent(mentorId)}/requests/${encodeURIComponent(requestId)}/status`,
    {
      method: 'POST',
      body: JSON.stringify({ status }),
    }
  )
  return response.request
}
