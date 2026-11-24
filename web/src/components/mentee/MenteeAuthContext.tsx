/**
 * Mentee Authentication Context (D95)
 *
 * Session state for the mentee dashboard. The session itself is an HttpOnly
 * cookie set by the Go API; this only mirrors whether one is live.
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { MenteeSession } from '@/types'
import {
  getMenteeSession,
  logoutMentee,
  requestMenteeLogin,
  verifyMenteeLogin,
} from '@/lib/mentee-api'
import analytics from '@/lib/analytics'

interface MenteeAuthContextValue {
  session: MenteeSession | null
  isLoading: boolean
  isAuthenticated: boolean
  requestLogin: (email: string) => Promise<{ success: boolean; message?: string }>
  verifyLogin: (token: string) => Promise<{ success: boolean; message?: string }>
  logout: () => Promise<void>
}

const MenteeAuthContext = createContext<MenteeAuthContextValue | null>(null)

export function MenteeAuthProvider({ children }: { children: ReactNode }): JSX.Element {
  const [session, setSession] = useState<MenteeSession | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    let mounted = true
    getMenteeSession()
      .then((current) => mounted && setSession(current))
      .catch(() => mounted && setSession(null))
      .finally(() => mounted && setIsLoading(false))
    return () => {
      mounted = false
    }
  }, [])

  const requestLogin = useCallback(async (email: string) => {
    const response = await requestMenteeLogin(email)
    return { success: response.success, message: response.message }
  }, [])

  const verifyLogin = useCallback(async (token: string) => {
    const response = await verifyMenteeLogin(token)
    if (response.success && response.session) {
      setSession(response.session)
    }
    return { success: response.success, message: response.message }
  }, [])

  const logout = useCallback(async () => {
    analytics.event(analytics.events.MENTEE_AUTH_LOGOUT, { outcome: 'submitted' })
    try {
      await logoutMentee()
    } finally {
      setSession(null)
    }
  }, [])

  // The mentee id is a random row id, not a capability, so it is safe as a
  // distinct id — the same treatment mentor sessions get.
  useEffect(() => {
    if (isLoading) return
    if (session?.mentee_id) {
      analytics.identify(`mentee:${session.mentee_id}`, { role: 'mentee' })
      return
    }
    // Without this, activity after logout is attributed to the previous mentee.
    analytics.reset()
  }, [isLoading, session?.mentee_id])

  const value = useMemo(
    () => ({
      session,
      isLoading,
      isAuthenticated: session !== null,
      requestLogin,
      verifyLogin,
      logout,
    }),
    [session, isLoading, requestLogin, verifyLogin, logout],
  )

  return <MenteeAuthContext.Provider value={value}>{children}</MenteeAuthContext.Provider>
}

export function useMenteeAuth(): MenteeAuthContextValue {
  const context = useContext(MenteeAuthContext)
  if (!context) {
    throw new Error('useMenteeAuth must be used within a MenteeAuthProvider')
  }
  return context
}
