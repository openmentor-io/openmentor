import {
  MENTOR_PASTEL_CLASSES,
  MENTOR_PASTEL_FALLBACK_CLASS,
  mentorPastelClass,
} from '@/lib/mentor-pastel'

describe('mentorPastelClass', () => {
  it('is deterministic — same key always gets the same classes', () => {
    expect(mentorPastelClass('john-doe')).toBe(mentorPastelClass('john-doe'))
    expect(mentorPastelClass('jane-smith')).toBe(mentorPastelClass('jane-smith'))
  })

  it('always returns one of the five pastel class pairs for a non-empty key', () => {
    const slugs = ['john-doe', 'jane-smith', 'a', 'some-very-long-mentor-slug', 'x-1', 'x-2', 'x-3']

    for (const slug of slugs) {
      expect(MENTOR_PASTEL_CLASSES).toContain(mentorPastelClass(slug))
    }
  })

  it('distributes different keys across multiple pastels', () => {
    const slugs = Array.from({ length: 50 }, (_, i) => `mentor-${i}`)
    const distinct = new Set(slugs.map(mentorPastelClass))

    // With 50 keys over 5 buckets, every pastel should be used
    expect(distinct.size).toBe(MENTOR_PASTEL_CLASSES.length)
  })

  it('falls back to the gray surface card for an empty key', () => {
    expect(mentorPastelClass('')).toBe(MENTOR_PASTEL_FALLBACK_CLASS)
  })

  it('every pastel pair contains a base and a hover-deepened tint', () => {
    for (const classes of MENTOR_PASTEL_CLASSES) {
      expect(classes).toMatch(/^bg-pastel-[a-z]+ hover:bg-pastel-[a-z]+-deep$/)
    }
  })
})
