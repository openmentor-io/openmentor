import { fixedAmount, isFree, parsePrice } from '@/lib/price'
import type { FiltersConfig, MentorTag } from '@/types'

// Tag groups, shared between the flat `tags` list (form options) and the
// catalog topic tabs (`categories`) below. Every tag belongs to exactly one
// group, so there is no "Others" catch-all. Keep these in sync with
// api/migrations/000009_modernise_tags.up.sql — a tag offered here but absent
// from the DB is silently dropped on save (that is what broke "Security").
const engineeringTags: MentorTag[] = [
  'Backend',
  'Frontend',
  'Mobile',
  'System Design',
  'Security',
  'QA & Test Automation',
]
const aiDataTags: MentorTag[] = [
  'AI/LLM Engineering',
  'Machine Learning',
  'Data Engineering',
  'Data & Analytics',
]
const infrastructureTags: MentorTag[] = [
  'DevOps/SRE',
  'Platform Engineering',
  'Cloud & Infrastructure',
  'Databases',
]
const productDesignTags: MentorTag[] = ['Product Management', 'UX/UI Design']
const leadershipTags: MentorTag[] = [
  'Engineering Management',
  'Tech Lead',
  'Staff+/IC Growth',
  'Career Growth',
  'Interview Prep',
  'Compensation & Negotiation',
  'Relocation & Working Abroad',
  'Project Management',
]
const businessTags: MentorTag[] = [
  'Entrepreneurship & Startups',
  'Freelancing & Consulting',
  'Developer Relations',
  'Technical Writing',
  'Marketing & Growth',
  'People & HR',
]

/**
 * The bucket label a stored price falls into — the SAME six strings byPrice
 * uses, in the same order, first match wins. This is the price dimension
 * analytics events send (`mentor_price_tier`, and `trackFilterChange('price',
 * label)` already spoke this vocabulary): six stable values, where the raw
 * price would be an unbounded-feeling ~1002-value dimension that turns every
 * PostHog breakdown into a long tail. Free deliberately reports 'Free', not
 * '≤$50', even though it matches both buckets.
 */
export function priceTierLabel(price: string): string {
  for (const [label, matches] of Object.entries(filters.byPrice)) {
    if (matches(price)) return label
  }
  // Unreachable: byPrice's Negotiable bucket collects everything unparseable.
  return 'Negotiable'
}

const filters: FiltersConfig = {
  tags: [
    ...engineeringTags,
    ...aiDataTags,
    ...infrastructureTags,
    ...productDesignTags,
    ...leadershipTags,
    ...businessTags,
  ],
  // Topic tabs for the catalog tab bar (redesign Phase A). A mentor matches a
  // category when they have at least one of its tags.
  categories: [
    { label: 'Engineering', tags: engineeringTags },
    { label: 'AI & Data', tags: aiDataTags },
    { label: 'Infrastructure', tags: infrastructureTags },
    { label: 'Product & Design', tags: productDesignTags },
    { label: 'Leadership & Career', tags: leadershipTags },
    { label: 'Business & Communication', tags: businessTags },
  ],
  experience: {
    '2-5 years': '2-5',
    '5-10 years': '5-10',
    '10+ years': '10+',
  },
  // Price filter buckets. The bucket LABELS are deliberately unchanged from the
  // free-text era (D3): they are the values `trackFilterChange('price', label)`
  // sends to PostHog, so renaming one silently splits a funnel.
  //
  // The predicates no longer classify free text — `mentors.price` is a closed
  // grammar since D87, so each is an integer comparison via lib/price.
  byPrice: {
    Free: (price) => isFree(price),
    // Free is in the cheapest bucket, as it was before: a mentee filtering for
    // "up to $50" wants the free sessions too.
    '≤$50': (price) => {
      if (isFree(price)) return true
      const amount = fixedAmount(price)
      return amount !== null && amount <= 50
    },
    '$50–100': (price) => {
      const amount = fixedAmount(price)
      return amount !== null && amount > 50 && amount <= 100
    },
    '$100–200': (price) => {
      const amount = fixedAmount(price)
      return amount !== null && amount > 100 && amount <= 200
    },
    '$200+': (price) => {
      const amount = fixedAmount(price)
      return amount !== null && amount > 200
    },
    // A value that does not parse lands here rather than nowhere, which is
    // where the free-text classifier put it too. Nothing in the database should
    // reach this branch since mentors_price_chk, but a bucket that silently
    // drops a mentor from every filter is worse than one that over-collects.
    Negotiable: (price) => {
      const value = parsePrice(price)
      return value === null || value.kind === 'negotiable'
    },
  },
}

export default filters
