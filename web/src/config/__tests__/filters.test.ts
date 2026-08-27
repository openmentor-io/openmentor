import filters, { priceTierLabel } from '@/config/filters'

/**
 * The price buckets had no test at all while they were free-text predicates.
 * They are integer comparisons over lib/price now (D87), which is the change
 * that makes them worth pinning: the failure mode is not a crash, it is a
 * mentor quietly missing from every filter, or a bucket label drifting and
 * splitting a PostHog funnel.
 */

const BUCKETS = filters.byPrice

/** Which buckets a stored price falls into, in declaration order. */
function bucketsFor(price: string): string[] {
  return Object.entries(BUCKETS)
    .filter(([, matches]) => matches(price))
    .map(([label]) => label)
}

describe('price bucket labels', () => {
  // These strings are the `label` argument of trackFilterChange('price', label)
  // in MentorsFilters, so they are PostHog property VALUES. Renaming one splits
  // a funnel silently — the panel keeps working and starts undercounting.
  it('is exactly the six labels the analytics events already use', () => {
    expect(Object.keys(BUCKETS)).toEqual([
      'Free',
      '≤$50',
      '$50–100',
      '$100–200',
      '$200+',
      'Negotiable',
    ])
  })
})

describe('price buckets', () => {
  it.each([
    ['Free', ['Free', '≤$50']],
    ['Negotiable', ['Negotiable']],
    ['$1', ['≤$50']],
    ['$50', ['≤$50']],
    ['$51', ['$50–100']],
    ['$100', ['$50–100']],
    ['$101', ['$100–200']],
    ['$200', ['$100–200']],
    ['$201', ['$200+']],
    ['$1000', ['$200+']],
  ])('puts %s in %p', (price, expected) => {
    expect(bucketsFor(price)).toEqual(expected)
  })

  // Free belongs in the cheapest bucket as well as its own: a mentee filtering
  // for "up to $50" wants the free sessions too. This was true before D87 and
  // is preserved deliberately.
  it('keeps Free inside the ≤$50 bucket', () => {
    expect(BUCKETS['≤$50']('Free')).toBe(true)
  })

  // The amount buckets partition the range: every fixed price lands in exactly
  // one, with no gap at a boundary and no double-count. A gap would drop a
  // mentor out of the catalog whenever that filter is applied.
  it('assigns every storable amount to exactly one amount bucket', () => {
    const amountBuckets = ['≤$50', '$50–100', '$100–200', '$200+']
    for (let amount = 1; amount <= 1000; amount++) {
      const hits = amountBuckets.filter((label) => BUCKETS[label](`$${amount}`))
      expect({ amount, hits }).toEqual({ amount, hits: [expect.any(String)] })
    }
  })

  // Nothing storable may fall through every bucket — that is a mentor who
  // vanishes from the catalog the moment any price filter is on.
  it.each(['Free', 'Negotiable', '$1', '$500', '$1000'])(
    'never drops %s from every bucket',
    (price) => {
      expect(bucketsFor(price).length).toBeGreaterThan(0)
    }
  )

  // mentors_price_chk makes these unstorable, so this is about degradation, not
  // correctness: over-collecting into Negotiable keeps the mentor reachable,
  // while matching nothing would hide them behind every filter.
  it.each(['$50 / hour', '', 'free', '50'])(
    'collects the pre-grammar value %p into Negotiable rather than nowhere',
    (price) => {
      expect(bucketsFor(price)).toEqual(['Negotiable'])
    }
  )
})

describe('priceTierLabel', () => {
  // These are the values `mentor_price_tier` sends to PostHog. The raw price
  // became an ~1002-value dimension under D87; the tier keeps the six-value
  // vocabulary the catalog filter events already speak.
  it.each([
    ['Free', 'Free'],
    ['Negotiable', 'Negotiable'],
    ['$1', '≤$50'],
    ['$50', '≤$50'],
    ['$137', '$100–200'],
    ['$1000', '$200+'],
    ['$50 / hour', 'Negotiable'],
  ])('labels %s as %s', (price, tier) => {
    expect(priceTierLabel(price)).toBe(tier)
  })

  it('reports Free as Free, not as the ≤$50 bucket it also matches', () => {
    expect(priceTierLabel('Free')).toBe('Free')
  })
})
