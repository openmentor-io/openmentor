import { formatDate, formatDateTime, formatRelativeTime } from '@/components/mentor-admin'

// P4.3: dates must use explicit, unambiguous en-US formats (e.g. "Jul 6, 2026"),
// never locale-dependent or DD.MM.YYYY output.

describe('formatDate', () => {
  it('formats a date as an unambiguous en-US date', () => {
    // Local noon avoids timezone-dependent date flips
    expect(formatDate('2026-07-06T12:00:00')).toBe('Jul 6, 2026')
  })

  it('does not produce DD.MM.YYYY-style output', () => {
    expect(formatDate('2026-02-01T12:00:00')).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/)
  })
})

describe('formatDateTime', () => {
  it('formats a datetime with the month spelled out', () => {
    expect(formatDateTime('2026-07-06T15:30:00')).toMatch(/^Jul 6, 2026, \d{1,2}:30 PM$/)
  })
})

describe('formatRelativeTime', () => {
  beforeEach(() => {
    jest.useFakeTimers()
    jest.setSystemTime(new Date('2026-07-06T12:00:00'))
  })

  afterEach(() => {
    jest.useRealTimers()
  })

  it('returns "just now" for the current time', () => {
    expect(formatRelativeTime('2026-07-06T11:59:40')).toBe('just now')
  })

  it('returns minutes ago with English pluralization', () => {
    expect(formatRelativeTime('2026-07-06T11:59:00')).toBe('1 minute ago')
    expect(formatRelativeTime('2026-07-06T11:55:00')).toBe('5 minutes ago')
  })

  it('returns hours ago', () => {
    expect(formatRelativeTime('2026-07-06T09:00:00')).toBe('3 hours ago')
  })

  it('returns "yesterday" for the previous day', () => {
    expect(formatRelativeTime('2026-07-05T11:00:00')).toBe('yesterday')
  })

  it('returns days and weeks ago', () => {
    expect(formatRelativeTime('2026-07-03T12:00:00')).toBe('3 days ago')
    expect(formatRelativeTime('2026-06-22T12:00:00')).toBe('2 weeks ago')
  })

  it('falls back to the unambiguous en-US date for older dates', () => {
    expect(formatRelativeTime('2026-01-15T12:00:00')).toBe('Jan 15, 2026')
  })
})
