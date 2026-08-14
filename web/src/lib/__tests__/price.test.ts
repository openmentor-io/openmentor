import {
  MAX_AMOUNT,
  MIN_AMOUNT,
  amountFromInput,
  formatPrice,
  isCanonicalPrice,
  isValidPrice,
  parsePrice,
  sanitizeAmountInput,
  type PriceValue,
} from '@/lib/price'

/**
 * This table is the same one as `parseCases` in api/pkg/price/price_test.go,
 * and the two must agree. The three implementations of the grammar — this one,
 * pkg/price, and the mentors_price_chk regexp — cannot import each other, so a
 * divergence only ever shows up as a save that 400s with no field attached, or
 * as a row the catalog cannot classify.
 *
 * Anything added here goes in the Go table too.
 */
const parseCases: { raw: string; want: PriceValue | null; why?: string }[] = [
  { raw: 'Free', want: { kind: 'free', amount: null } },
  { raw: 'Negotiable', want: { kind: 'negotiable', amount: null } },

  { raw: '$1', want: { kind: 'fixed', amount: 1 }, why: 'MIN_AMOUNT' },
  { raw: '$1000', want: { kind: 'fixed', amount: 1000 }, why: 'MAX_AMOUNT, inclusive' },
  // Every distinct amount live in production on 2026-08-13.
  { raw: '$10', want: { kind: 'fixed', amount: 10 } },
  { raw: '$20', want: { kind: 'fixed', amount: 20 } },
  { raw: '$30', want: { kind: 'fixed', amount: 30 } },
  { raw: '$40', want: { kind: 'fixed', amount: 40 } },
  { raw: '$50', want: { kind: 'fixed', amount: 50 } },
  { raw: '$60', want: { kind: 'fixed', amount: 60 } },
  { raw: '$70', want: { kind: 'fixed', amount: 70 } },
  { raw: '$80', want: { kind: 'fixed', amount: 80 } },
  { raw: '$90', want: { kind: 'fixed', amount: 90 } },
  { raw: '$100', want: { kind: 'fixed', amount: 100 } },
  { raw: '$120', want: { kind: 'fixed', amount: 120 } },
  { raw: '$150', want: { kind: 'fixed', amount: 150 } },
  { raw: '$200', want: { kind: 'fixed', amount: 200 } },

  { raw: '', want: null, why: 'empty is what an unset column used to save as' },
  { raw: '$', want: null, why: 'the sigil alone' },
  { raw: '$0', want: null, why: 'a free session is Free, not zero dollars' },
  { raw: '$00', want: null, why: 'leading zero' },
  { raw: '$01', want: null, why: 'leading zero' },
  { raw: '$050', want: null, why: 'a second spelling of $50' },
  { raw: '$1001', want: null, why: 'one over the cap' },
  { raw: '$99999', want: null, why: 'far over the cap' },
  { raw: '$+50', want: null, why: 'Number() would accept the sign' },
  { raw: '$-50', want: null, why: 'Number() would accept the sign' },
  { raw: '50', want: null, why: "bare digits: what mapPrice's passthrough could emit" },
  { raw: '$50.00', want: null, why: 'no decimals' },
  { raw: '$1,000', want: null, why: 'no thousands separator' },
  { raw: '$50 / hour', want: null, why: 'the free-text shape D3 allowed' },
  { raw: '$100 / hour', want: null, why: 'the free-text shape D3 allowed' },
  { raw: '$ 50', want: null, why: 'space after the sigil' },
  { raw: ' Free', want: null, why: 'parsePrice does not trim' },
  { raw: 'Free ', want: null, why: 'parsePrice does not trim' },
  { raw: 'Free\n', want: null, why: 'a trailing newline is still not Free' },
  { raw: 'free', want: null, why: 'parsePrice does not case-fold' },
  { raw: 'FREE', want: null, why: 'parsePrice does not case-fold' },
  { raw: 'negotiable', want: null, why: 'parsePrice does not case-fold' },
  { raw: 'Negotiable price', want: null, why: 'the canonical word is the whole value' },
  { raw: '$٥٠', want: null, why: 'Arabic-Indic digits are not ASCII digits' },
  { raw: '€50', want: null, why: 'one currency only' },
]

describe('parsePrice', () => {
  it.each(parseCases)('parses $raw', ({ raw, want, why }) => {
    expect(parsePrice(raw)).toEqual(want)
    expect(isCanonicalPrice(raw)).toBe(want !== null)
    if (why !== undefined && want === null) {
      // Keeps the reason attached to the case in the failure output.
      expect(`${raw}: ${why}`).toContain(why)
    }
  })
})

describe('formatPrice', () => {
  it.each(parseCases.filter((c) => c.want !== null))(
    'round-trips $raw byte for byte',
    ({ raw, want }) => {
      expect(formatPrice(want as PriceValue)).toBe(raw)
    }
  )

  // Refuses to render a pair parsePrice could not have produced, so a form that
  // submits it writes a price the database constraint rejects rather than a
  // plausible-looking wrong one.
  it.each([
    [{ kind: 'fixed', amount: null }, 'no amount chosen yet'],
    [{ kind: 'fixed', amount: 0 }, 'below MIN_AMOUNT'],
    [{ kind: 'fixed', amount: 1001 }, 'above MAX_AMOUNT'],
    [{ kind: 'fixed', amount: -50 }, 'negative'],
    [{ kind: 'fixed', amount: 50.5 }, 'not whole'],
  ] as [PriceValue, string][])('returns null for %p (%s)', (value) => {
    expect(formatPrice(value)).toBeNull()
  })

  // Kind is the discriminator, not amount === 0: a stale amount left over from
  // switching away from "fixed" must not leak into the rendered value.
  it('ignores a stale amount on the amount-less kinds', () => {
    expect(formatPrice({ kind: 'free', amount: 120 })).toBe('Free')
    expect(formatPrice({ kind: 'negotiable', amount: 120 })).toBe('Negotiable')
  })
})

describe('isValidPrice', () => {
  it('accepts the amount-less kinds with no amount', () => {
    expect(isValidPrice({ kind: 'free', amount: null })).toBe(true)
    expect(isValidPrice({ kind: 'negotiable', amount: null })).toBe(true)
  })

  it('requires a whole in-range amount for fixed', () => {
    expect(isValidPrice({ kind: 'fixed', amount: null })).toBe(false)
    expect(isValidPrice({ kind: 'fixed', amount: 0 })).toBe(false)
    expect(isValidPrice({ kind: 'fixed', amount: 1001 })).toBe(false)
    expect(isValidPrice({ kind: 'fixed', amount: 50 })).toBe(true)
  })
})

describe('sanitizeAmountInput', () => {
  it.each([
    ['', '', 'an empty box stays empty'],
    ['50', '50', 'digits pass through'],
    ['abc', '', 'letters are not an amount'],
    ['5a0', '50', 'letters are dropped from between digits'],
    ['0', '', 'a lone zero clears rather than sitting there as an invalid amount'],
    ['007', '7', 'leading zeros are dropped'],
    ['$50', '50', 'the sigil is a prefix element, never part of the value'],
    ['50.5', '505', 'decimals are not a thing this grammar has'],
    ['-50', '50', 'a sign is not part of an amount'],
    ['1000', '1000', 'MAX_AMOUNT must stay typable'],
    ['12345', '1234', 'capped at the digits MAX_AMOUNT has'],
    ['  50  ', '50', 'whitespace is dropped'],
  ])('sanitizes %p to %p (%s)', (raw, want) => {
    expect(sanitizeAmountInput(raw)).toBe(want)
  })
})

describe('amountFromInput', () => {
  it('reports null for an empty box rather than 0', () => {
    expect(amountFromInput('')).toBeNull()
    expect(amountFromInput('abc')).toBeNull()
    expect(amountFromInput('0')).toBeNull()
  })

  it('reads the sanitized digits', () => {
    expect(amountFromInput('50')).toBe(50)
    expect(amountFromInput('$120')).toBe(120)
  })
})

describe('bounds', () => {
  it('forbids a zero price at the floor', () => {
    expect(MIN_AMOUNT).toBe(1)
    expect(MAX_AMOUNT).toBe(1000)
  })
})
