/**
 * Deterministic pastel card colors (redesign Phase B).
 *
 * The five hues are sampled from the Figma concept catalog
 * (docs/design-reference/figma-exports/Frame 2131329156.png:
 * #BDEEFF / #FFE7BD / #BDFFBD / #C2BDFF / #FFBDDF) and tuned to the
 * "Fresh Signal" brand: desaturated and warmed toward --om-paper-dim
 * (#F7F6F2) so they sit quietly on the paper background. Hex values
 * live in tailwind.config.js under `colors.pastel`.
 *
 * Contrast (WCAG AA, verified):
 * - ink #161A20 on every base/deep pastel: >= 11.0:1
 * - ink-mute #4A5160 (card meta text) on every base/deep pastel: >= 5.0:1
 * - brand navy #132A52 on the white/75 "New" pill over any pastel: >= 12.7:1
 *
 * A mentor keeps the same color across pages and reloads: the class is
 * picked by an FNV-1a hash of the mentor slug. The neutral gray
 * `surface` card is kept as the *fallback* (empty key), not as a sixth
 * rotation color, to match the fully-pastel grid of the design.
 */

/** Base pastel + deepened hover tint, as full class strings so Tailwind can see them. */
export const MENTOR_PASTEL_CLASSES = [
  'bg-pastel-sky hover:bg-pastel-sky-deep',
  'bg-pastel-sand hover:bg-pastel-sand-deep',
  'bg-pastel-sage hover:bg-pastel-sage-deep',
  'bg-pastel-lavender hover:bg-pastel-lavender-deep',
  'bg-pastel-rose hover:bg-pastel-rose-deep',
] as const

/** Phase A gray card, kept as the fallback when no stable key exists. */
export const MENTOR_PASTEL_FALLBACK_CLASS = 'bg-surface hover:bg-line/60'

/**
 * Pick the pastel classes for a mentor.
 * @param key - Stable mentor identifier (slug). Same key -> same color.
 */
export function mentorPastelClass(key: string): string {
  if (!key) {
    return MENTOR_PASTEL_FALLBACK_CLASS
  }

  // FNV-1a 32-bit
  let hash = 0x811c9dc5
  for (let i = 0; i < key.length; i++) {
    hash ^= key.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193) >>> 0
  }

  return MENTOR_PASTEL_CLASSES[hash % MENTOR_PASTEL_CLASSES.length]
}
