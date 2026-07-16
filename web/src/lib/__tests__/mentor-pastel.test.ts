import {
  MENTOR_INITIALS_CLASSES,
  MENTOR_PASTEL_GRAD_CLASSES,
  MENTOR_PASTEL_NEUTRAL_GRAD_CLASS,
  mentorInitialsClass,
  mentorPastelGradClass,
} from '@/lib/mentor-pastel'

describe('mentorPastelGradClass', () => {
  it('is deterministic — same key always gets the same gradient', () => {
    expect(mentorPastelGradClass('john-doe')).toBe(mentorPastelGradClass('john-doe'))
    expect(mentorPastelGradClass('jane-smith')).toBe(mentorPastelGradClass('jane-smith'))
  })

  it('always returns one of the five pastel gradient classes for a non-empty key', () => {
    const slugs = ['john-doe', 'jane-smith', 'a', 'some-very-long-mentor-slug', 'x-1', 'x-2', 'x-3']

    for (const slug of slugs) {
      expect(MENTOR_PASTEL_GRAD_CLASSES).toContain(mentorPastelGradClass(slug))
    }
  })

  it('distributes different keys across all pastels', () => {
    const slugs = Array.from({ length: 50 }, (_, i) => `mentor-${i}`)
    const distinct = new Set(slugs.map(mentorPastelGradClass))

    // With 50 keys over 5 buckets, every pastel should be used
    expect(distinct.size).toBe(MENTOR_PASTEL_GRAD_CLASSES.length)
  })

  it('falls back to the neutral paper gradient for an empty key', () => {
    expect(mentorPastelGradClass('')).toBe(MENTOR_PASTEL_NEUTRAL_GRAD_CLASS)
  })

  it('every gradient class is a bg-pastel-*-grad utility', () => {
    for (const cls of MENTOR_PASTEL_GRAD_CLASSES) {
      expect(cls).toMatch(/^bg-pastel-[a-z]+-grad$/)
    }
  })
})

describe('mentorInitialsClass', () => {
  it('is deterministic — same key always gets the same circle fill', () => {
    expect(mentorInitialsClass('john-doe')).toBe(mentorInitialsClass('john-doe'))
  })

  it('always returns navy or cobalt', () => {
    const slugs = ['john-doe', 'jane-smith', 'a', 'x-1', 'x-2', 'x-3', '']

    for (const slug of slugs) {
      expect(MENTOR_INITIALS_CLASSES).toContain(mentorInitialsClass(slug))
    }
  })

  it('uses both fills across many keys', () => {
    const slugs = Array.from({ length: 30 }, (_, i) => `mentor-${i}`)
    const distinct = new Set(slugs.map(mentorInitialsClass))

    expect(distinct.size).toBe(MENTOR_INITIALS_CLASSES.length)
  })
})
