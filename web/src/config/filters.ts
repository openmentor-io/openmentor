import type { FiltersConfig, MentorTag } from '@/types'

/**
 * Extract a numeric amount from a free-text price (e.g. "$100 / hour" -> 100).
 * Returns null when no number is present.
 * Exported for the mentor card meta row ("$50" / "FREE" / "NEGOTIABLE").
 */
export function parsePriceAmount(price: string): number | null {
  const match = price.replace(/[,\s]/g, '').match(/(\d+(?:\.\d+)?)/)
  return match ? parseFloat(match[1]) : null
}

/** Whether a free-text price means "free" (see DECISIONS D3). */
export function isPriceFree(price: string): boolean {
  return /free/i.test(price) || parsePriceAmount(price) === 0
}

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
  // Suggested options for legacy price selects. The price field itself is
  // free text (see DECISIONS D3) — these are interim values until the forms
  // switch to a plain text input.
  price: ['Free', '$50', '$100', '$150', '$200', 'Negotiable'],
  experience: {
    '2-5 years': '2-5',
    '5-10 years': '5-10',
    '10+ years': '10+',
  },
  // Price filter buckets (DECISIONS D3). `mentors.price` is free text, so
  // each bucket is a predicate that classifies a price string.
  byPrice: {
    Free: (price) => isPriceFree(price),
    '≤$50': (price) => {
      if (isPriceFree(price)) return true
      const amount = parsePriceAmount(price)
      return amount !== null && amount <= 50
    },
    '$50–100': (price) => {
      const amount = parsePriceAmount(price)
      return amount !== null && amount > 50 && amount <= 100
    },
    '$100–200': (price) => {
      const amount = parsePriceAmount(price)
      return amount !== null && amount > 100 && amount <= 200
    },
    '$200+': (price) => {
      const amount = parsePriceAmount(price)
      return amount !== null && amount > 200
    },
    Negotiable: (price) =>
      /negotiable/i.test(price) || (parsePriceAmount(price) === null && !isPriceFree(price)),
  },
}

export default filters
