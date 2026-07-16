/**
 * Utility functions for Mentor Admin
 */

/**
 * Format date (e.g., "Jul 7, 2026")
 */
export function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString('en-US', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

/**
 * Format date with time (e.g., "Jul 7, 2026, 10:30 AM")
 */
export function formatDateTime(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleString('en-US', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/**
 * Pluralize a time unit and append "ago" (e.g., "2 days ago")
 */
function timeAgo(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? '' : 's'} ago`
}

/**
 * Format relative time (e.g., "just now", "5 minutes ago", "yesterday")
 */
export function formatRelativeTime(dateString: string): string {
  const date = new Date(dateString)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffDays === 0) {
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
    if (diffHours === 0) {
      const diffMinutes = Math.floor(diffMs / (1000 * 60))
      if (diffMinutes < 1) return 'just now'
      return timeAgo(diffMinutes, 'minute')
    }
    return timeAgo(diffHours, 'hour')
  }

  if (diffDays === 1) return 'yesterday'
  if (diffDays < 7) return timeAgo(diffDays, 'day')
  if (diffDays < 30) {
    const weeks = Math.floor(diffDays / 7)
    return timeAgo(weeks, 'week')
  }

  return formatDate(dateString)
}

/**
 * Compact mono timestamp for request rows (design 07: "2H AGO", "1D AGO",
 * "MAR 2026"). Meant to be rendered in the .meta-mono style (CAPS).
 */
export function formatCompactTime(dateString: string): string {
  const date = new Date(dateString)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMinutes = Math.floor(diffMs / (1000 * 60))

  if (diffMinutes < 1) return 'NOW'
  if (diffMinutes < 60) return `${diffMinutes}M AGO`

  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) return `${diffHours}H AGO`

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 7) return `${diffDays}D AGO`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}W AGO`

  return date
    .toLocaleDateString('en-US', { month: 'short', year: 'numeric' })
    .toUpperCase()
}

/**
 * First letters of the first two name words ("Daria Kovalenko" -> "DK").
 */
export function nameInitials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('')
}
