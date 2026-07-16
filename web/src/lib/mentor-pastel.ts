/**
 * Deterministic pastel card colors (redesign catalog).
 *
 * The five hues are sampled from the design reference and tuned to the
 * "Fresh Signal" brand: desaturated and warmed toward --om-paper-dim
 * (#F7F6F2) so they sit quietly on the paper background. Hex values live
 * in tailwind.config.js under `colors.pastel`; the card photo blocks
 * render them as top→bottom gradients (base → deep) via the
 * `bg-pastel-*-grad` backgroundImage utilities.
 *
 * Contrast (WCAG AA, verified):
 * - ink #161A20 on every base/deep pastel: >= 11.0:1
 * - ink-mute #4A5160 (card meta text) on every base/deep pastel: >= 5.0:1
 * - brand navy #132A52 on the white/90 badge over any pastel: >= 12.7:1
 *
 * A mentor keeps the same color across pages and reloads: the class is
 * picked by an FNV-1a hash of the mentor slug. The neutral paper gradient
 * is kept as the *fallback* (empty key), not as a sixth rotation color,
 * to match the fully-pastel grid of the design.
 */

/** Pastel photo-block gradients, as full class strings so Tailwind can see them. */
export const MENTOR_PASTEL_GRAD_CLASSES = [
  'bg-pastel-sky-grad',
  'bg-pastel-sand-grad',
  'bg-pastel-sage-grad',
  'bg-pastel-lavender-grad',
  'bg-pastel-rose-grad',
] as const

/** Neutral paper gradient, kept as the fallback when no stable key exists. */
export const MENTOR_PASTEL_NEUTRAL_GRAD_CLASS = 'bg-pastel-neutral-grad'

/** Initials circle fills (fallback B): navy or cobalt by slug hash parity. */
export const MENTOR_INITIALS_CLASSES = ['bg-brand-navy', 'bg-brand-cobalt'] as const

/** FNV-1a 32-bit hash — the single source of card color determinism. */
function fnv1a(key: string): number {
  let hash = 0x811c9dc5
  for (let i = 0; i < key.length; i++) {
    hash ^= key.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193) >>> 0
  }
  return hash
}

/**
 * Pick the pastel gradient class for a mentor card photo block.
 * @param key - Stable mentor identifier (slug). Same key -> same gradient.
 */
export function mentorPastelGradClass(key: string): string {
  if (!key) {
    return MENTOR_PASTEL_NEUTRAL_GRAD_CLASS
  }

  return MENTOR_PASTEL_GRAD_CLASSES[fnv1a(key) % MENTOR_PASTEL_GRAD_CLASSES.length]
}

/**
 * Pick the initials-circle fill (navy/cobalt) for a mentor without a photo.
 * @param key - Stable mentor identifier (slug). Same key -> same fill.
 */
export function mentorInitialsClass(key: string): string {
  return MENTOR_INITIALS_CLASSES[fnv1a(key) % MENTOR_INITIALS_CLASSES.length]
}
