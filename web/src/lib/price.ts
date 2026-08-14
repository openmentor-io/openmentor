/**
 * The browser copy of the mentor price grammar.
 *
 * A price is one of three things — free, negotiable, or a whole dollar amount
 * between MIN_AMOUNT and MAX_AMOUNT — and the stored form is its canonical
 * spelling: `Free`, `Negotiable` or `$N`. This module is the single place that
 * pair is turned into that string and back, so a display site never re-derives
 * it with a regexp of its own. Two byte-identical `parsePriceAmount`
 * implementations (config/filters.ts and ui/PriceBadge.tsx) are what this
 * replaces (D87).
 *
 * It mirrors api/pkg/price and must stay at least as strict as the API's
 * `price` binding tag: a value the browser accepts but the API rejects comes
 * back as a 400 with no field attached, which the profile and registration
 * forms surface as an unrecoverable generic error. The two cannot import each
 * other, so src/lib/__tests__/price.test.ts carries the same case table as
 * api/pkg/price/price_test.go and pins this copy's shape.
 */

export type PriceKind = 'free' | 'negotiable' | 'fixed'

/**
 * Inclusive bounds on a fixed amount. MIN_AMOUNT is 1, not 0: a mentor who
 * charges nothing has kind `free`, which is a different statement from charging
 * zero dollars. `mentors_price_chk` and price.MinAmount enforce the same floor.
 */
export const MIN_AMOUNT = 1
export const MAX_AMOUNT = 1000

/** The digits of MAX_AMOUNT — how many an amount input needs to accept. */
const MAX_AMOUNT_DIGITS = String(MAX_AMOUNT).length

export interface PriceValue {
  kind: PriceKind
  /** Null for `free` and `negotiable`. Read `kind` first; 0 is not "free". */
  amount: number | null
}

const FREE_TEXT = 'Free'
const NEGOTIABLE_TEXT = 'Negotiable'

/** Whether n is a whole amount this grammar can store. */
export function isValidAmount(amount: number | null): amount is number {
  return (
    amount !== null &&
    Number.isInteger(amount) &&
    amount >= MIN_AMOUNT &&
    amount <= MAX_AMOUNT
  )
}

/** Whether a {kind, amount} pair is complete enough to submit. */
export function isValidPrice(value: PriceValue): boolean {
  if (value.kind === 'fixed') return isValidAmount(value.amount)
  return value.kind === 'free' || value.kind === 'negotiable'
}

/**
 * Parses a canonical stored price. Deliberately strict — no trimming, no case
 * folding — for the same reason api/pkg/price.Parse is: the forms serialize
 * from a {kind, amount} pair, so a value needing cleanup is a bug in a caller,
 * and a lenient parser would hide it.
 *
 * Returns null for anything outside the grammar, which display sites use to
 * fall back to rendering the raw string rather than showing nothing.
 */
export function parsePrice(raw: string): PriceValue | null {
  if (raw === FREE_TEXT) return { kind: 'free', amount: null }
  if (raw === NEGOTIABLE_TEXT) return { kind: 'negotiable', amount: null }

  // A leading zero would give "$50" a second spelling, and "$0" is below
  // MIN_AMOUNT anyway.
  if (!/^\$[1-9][0-9]*$/.test(raw)) return null

  const amount = Number(raw.slice(1))
  return isValidAmount(amount) ? { kind: 'fixed', amount } : null
}

/** Whether raw is a price the API would store verbatim. */
export function isCanonicalPrice(raw: string): boolean {
  return parsePrice(raw) !== null
}

/**
 * The dollar figure when the stored price is a fixed one, and null for `Free`,
 * `Negotiable` or anything unparseable. This is what the catalog price buckets
 * and the card/badge/chip renderers compare against, so none of them needs a
 * parser of its own.
 */
export function fixedAmount(raw: string): number | null {
  const value = parsePrice(raw)
  return value !== null && value.kind === 'fixed' ? value.amount : null
}

/** Whether a stored price means the session is free. */
export function isFree(raw: string): boolean {
  return parsePrice(raw)?.kind === 'free'
}

/**
 * Renders the canonical stored spelling, or null when the pair is not a
 * complete price. Null rather than a plausible-looking fallback: a form that
 * submits it would be writing a price the mentor did not choose.
 */
export function formatPrice(value: PriceValue): string | null {
  if (value.kind === 'free') return FREE_TEXT
  if (value.kind === 'negotiable') return NEGOTIABLE_TEXT
  if (!isValidAmount(value.amount)) return null
  return `$${value.amount}`
}

/**
 * Keystroke-level cleanup for the amount input: digits only, no leading zero,
 * no more digits than MAX_AMOUNT has.
 *
 * The input is a `type="text"` with `inputMode="numeric"` rather than a
 * `type="number"`, which accepts `e`, `+`, `-` and `.`, mutates on scroll-wheel,
 * and reports `''` for a value it considers invalid — so "empty" and "garbage"
 * become indistinguishable and the required-field message ends up wrong.
 */
export function sanitizeAmountInput(raw: string): string {
  return raw.replace(/[^0-9]/g, '').replace(/^0+/, '').slice(0, MAX_AMOUNT_DIGITS)
}

/**
 * The amount as a number, or null when the box is empty. Used to drive
 * validation off the input's own text without a second source of truth.
 */
export function amountFromInput(raw: string): number | null {
  const digits = sanitizeAmountInput(raw)
  return digits === '' ? null : Number(digits)
}
