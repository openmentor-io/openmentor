/**
 * Utility helpers for the admin moderation portal.
 */

import type { MentorModerationStatus } from '@/types'

/**
 * Mentor lifecycle status pill classes (on-system, per redesign spec):
 * draft = deep paper, pending = sky pastel, active = mint tint,
 * declined = deep paper muted, inactive = plain surface.
 */
export function moderationStatusBadgeClass(status: MentorModerationStatus): string {
  const base =
    'inline-flex items-center rounded-full px-3 py-1.5 font-mono text-[11px] font-bold uppercase tracking-[0.05em]'

  switch (status) {
    case 'draft':
      return `${base} bg-surface-deep text-ink`
    case 'pending':
      return `${base} bg-pastel-sky text-brand-navy`
    case 'active':
      return `${base} bg-brand-mint/15 text-mint-ink`
    case 'declined':
      return `${base} bg-surface-deep text-ink-mute`
    case 'inactive':
    default:
      return `${base} border border-line bg-surface text-ink-mute`
  }
}
