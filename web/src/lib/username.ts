/**
 * Username (public name for the mentor slug) client helpers.
 *
 * Mirrors the Go validation in api/pkg/slug/username.go: lowercase, ASCII
 * `[a-z0-9]` with single hyphens between segments, length 3–40. The frontend
 * is advisory — the Go API is authoritative (and the DB unique constraint is
 * the final backstop) — but keeping the rules in sync gives instant feedback.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type { UsernameAvailabilityResult } from '@/types'

export const USERNAME_MIN_LENGTH = 3
export const USERNAME_MAX_LENGTH = 40

/**
 * Turn an arbitrary string (e.g. a display name) into a valid-ish username
 * candidate: transliterate-free lowercase, non-alphanumerics become hyphens,
 * collapsed and trimmed. Used to prefill the registration username field.
 */
export function slugifyUsername(input: string): string {
  return input
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '') // strip combining diacritics
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-') // any run of non-alphanumerics -> single hyphen
    .replace(/^-+|-+$/g, '') // trim leading/trailing hyphens
    .slice(0, USERNAME_MAX_LENGTH)
    .replace(/-+$/g, '') // re-trim if the slice landed on a hyphen
}

/** Local format check (does not consult the server or reserved-word list). */
export function isUsernameFormatValid(username: string): boolean {
  if (username.length < USERNAME_MIN_LENGTH || username.length > USERNAME_MAX_LENGTH) {
    return false
  }
  return /^[a-z0-9]+(-[a-z0-9]+)*$/.test(username)
}

export type AvailabilityState =
  | { status: 'idle' }
  | { status: 'checking' }
  | { status: 'available'; username: string }
  | { status: 'unavailable'; username: string; reason: 'taken' | 'invalid' | 'reserved'; message?: string }
  | { status: 'error' }

const DEBOUNCE_MS = 400

/**
 * Debounced username availability hook. Calls the given endpoint with the
 * candidate as `?u=` after a 400 ms pause, cancelling in-flight requests via
 * AbortController so only the latest keystroke wins.
 *
 * `endpoint` is a BFF proxy path, e.g. '/api/username-availability' (public)
 * or '/api/mentor/username' (session).
 */
export function useUsernameAvailability(candidate: string, endpoint: string): AvailabilityState {
  const [state, setState] = useState<AvailabilityState>({ status: 'idle' })
  const abortRef = useRef<AbortController | null>(null)

  const check = useCallback(
    async (value: string, signal: AbortSignal) => {
      try {
        const res = await fetch(`${endpoint}?u=${encodeURIComponent(value)}`, { signal })
        if (signal.aborted) return
        if (!res.ok) {
          setState({ status: 'error' })
          return
        }
        const data = (await res.json()) as UsernameAvailabilityResult
        if (signal.aborted) return
        if (data.available) {
          setState({ status: 'available', username: data.username })
        } else {
          setState({
            status: 'unavailable',
            username: data.username,
            reason: data.reason ?? 'invalid',
            message: data.message,
          })
        }
      } catch (error) {
        if ((error as Error).name === 'AbortError') return
        setState({ status: 'error' })
      }
    },
    [endpoint]
  )

  useEffect(() => {
    const trimmed = candidate.trim()
    // Cancel any in-flight request when the input changes.
    abortRef.current?.abort()

    if (trimmed === '') {
      setState({ status: 'idle' })
      return
    }
    // Fail fast on obvious format errors without hitting the network.
    if (!isUsernameFormatValid(trimmed)) {
      setState({ status: 'unavailable', username: trimmed, reason: 'invalid' })
      return
    }

    setState({ status: 'checking' })
    const controller = new AbortController()
    abortRef.current = controller
    const timeoutId = window.setTimeout(() => {
      void check(trimmed, controller.signal)
    }, DEBOUNCE_MS)

    return () => {
      window.clearTimeout(timeoutId)
      controller.abort()
    }
  }, [candidate, check])

  return state
}
