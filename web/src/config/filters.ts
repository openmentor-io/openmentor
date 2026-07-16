import type { FiltersConfig } from '@/types'

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

// Tag groups (shared between the legacy grouped dropdowns config and the
// topic tab categories below)
const developmentTags = ['Backend', 'Frontend', 'iOS', 'Android', 'System Design', 'Code Review']
const managementTags = ['Agile', 'Team Lead/Management', 'Project Management', 'Product Management']
const opsTags = ['DevOps/SRE', 'Databases', 'Networking', 'Cloud', 'Security']
const hrTags = ['HR', 'Career', 'Interview prep', 'Entrepreneurship', 'DevRel']
const marketingTags = ['Marketing', 'Content/Copy']

const filters: FiltersConfig = {
  tags: [
    'Backend',
    'Frontend',
    'Code Review',
    'System Design',
    'UX/UI/Design',
    'iOS',
    'Android',
    'QA',
    'Marketing',
    'Content/Copy',
    'Databases',
    'Data Science/ML',
    'Analytics',
    'Networking',
    'Cloud',
    'Security',
    'DevOps/SRE',
    'Agile',
    'Team Lead/Management',
    'Project Management',
    'Product Management',
    'Entrepreneurship',
    'DevRel',
    'HR',
    'Career',
    'Interview prep',
    'Other',
  ],
  byTags: {
    development: developmentTags,
    management: managementTags,
    ops: opsTags,
    hr: hrTags,
    marketing: marketingTags,
    rest: ['Data Science/ML', 'UX/UI/Design', 'QA', 'Analytics', 'Other'],
  },
  // Topic tabs for the catalog tab bar (redesign Phase A). A mentor matches a
  // category when they have at least one of its tags. Every tag from `tags`
  // belongs to exactly one category; "Others" aggregates what's left.
  categories: [
    { label: 'Development', tags: developmentTags },
    { label: 'Management', tags: managementTags },
    { label: 'DevOps', tags: opsTags },
    { label: 'HR', tags: hrTags },
    { label: 'Marketing', tags: marketingTags },
    { label: 'Data Science', tags: ['Data Science/ML', 'Analytics'] },
    { label: 'Design', tags: ['UX/UI/Design'] },
    { label: 'Others', tags: ['QA', 'Other'] },
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
