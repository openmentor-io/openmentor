import { ALL_TAGS, TAG_PAGES, tagBySlug } from '@/config/tags'

// Guards the invariants that keep /mentors/<slug> URLs safe: unique,
// URL-clean, never colliding with the legacy getmentor.dev numeric-slug
// redirect (next.config.js), and round-trippable back to a tag.
describe('TAG_PAGES', () => {
  const slugs = ALL_TAGS.map((tag) => TAG_PAGES[tag].slug)

  it('has a unique slug per tag', () => {
    expect(new Set(slugs).size).toBe(slugs.length)
  })

  it('only uses lowercase alphanumerics with single hyphens between words', () => {
    for (const slug of slugs) {
      expect(slug).toMatch(/^[a-z0-9]+(-[a-z0-9]+)*$/)
    }
  })

  it('never ends in a digit, so the legacy numeric-slug redirect cannot swallow it', () => {
    // next.config.js redirects /:slug([a-z-]+\d+) to /mentor/:slug — a tag
    // slug ending in a digit would be silently redirected away from itself.
    for (const slug of slugs) {
      expect(slug).not.toMatch(/\d$/)
    }
  })

  it('gives every tag a non-empty blurb', () => {
    for (const tag of ALL_TAGS) {
      expect(TAG_PAGES[tag].blurb.trim().length).toBeGreaterThan(0)
    }
  })

  it('round-trips every tag through tagBySlug', () => {
    for (const tag of ALL_TAGS) {
      expect(tagBySlug(TAG_PAGES[tag].slug)).toBe(tag)
    }
  })

  it('returns null for an unknown slug', () => {
    expect(tagBySlug('not-a-real-tag')).toBeNull()
  })
})
