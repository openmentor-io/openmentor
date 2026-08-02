import filters from '@/config/filters'
import type { MentorTag } from '@/types'

export interface TagPage {
  /**
   * URL segment for /mentors/<slug>. PERMANENT once shipped — changing one
   * retires an indexed URL, so rename the label rather than the slug.
   */
  slug: string
  /**
   * What this expertise covers, in one line. Feeds the landing page intro and
   * its meta description, so it has to read as a sentence, not a keyword list.
   */
  blurb: string
}

/**
 * Landing-page metadata for every mentor tag.
 *
 * Typed `Record<MentorTag, TagPage>` deliberately: adding a tag to the union
 * without giving it a slug fails the type check. That is the same drift guard
 * D30 put on the taxonomy itself, after `Security` shipped in the UI but not
 * the database and was silently dropped.
 */
export const TAG_PAGES: Record<MentorTag, TagPage> = {
  // Engineering
  Backend: {
    slug: 'backend',
    blurb:
      'Server-side systems, APIs and the services behind them — architecture, data modelling, reliability and the trade-offs that show up at scale.',
  },
  Frontend: {
    slug: 'frontend',
    blurb:
      'Browser-side craft — component architecture, state, performance, accessibility, and shipping interfaces people can actually use.',
  },
  Mobile: {
    slug: 'mobile',
    blurb:
      'iOS and Android work — platform conventions, release cycles, offline behaviour and the constraints native apps live under.',
  },
  'System Design': {
    slug: 'system-design',
    blurb:
      'Designing systems that hold up — decomposition, data flow, failure modes, and the interview format that tests all three.',
  },
  Security: {
    slug: 'security',
    blurb:
      'Application and infrastructure security — threat modelling, secure defaults, reviews, and building it in rather than bolting it on.',
  },
  'QA & Test Automation': {
    slug: 'qa-test-automation',
    blurb:
      'Testing strategy and automation — what to test, at which level, and how to keep a suite fast enough that people still trust it.',
  },

  // AI & Data
  'AI/LLM Engineering': {
    slug: 'ai-llm-engineering',
    blurb:
      'Building with large language models — prompting, retrieval, evaluation, cost and latency, and knowing when not to use one at all.',
  },
  'Machine Learning': {
    slug: 'machine-learning',
    blurb:
      'Models end to end — framing the problem, features, training, evaluation, and what changes once the thing is in production.',
  },
  'Data Engineering': {
    slug: 'data-engineering',
    blurb:
      'Pipelines and platforms — ingestion, transformation, orchestration, quality, and data other teams can actually rely on.',
  },
  'Data & Analytics': {
    slug: 'data-analytics',
    blurb:
      'Turning data into decisions — metrics that mean something, analysis, experimentation, and communicating the result.',
  },

  // Infrastructure
  'DevOps/SRE': {
    slug: 'devops-sre',
    blurb:
      'Running things in production — CI/CD, observability, incidents, on-call, and the reliability practices that hold it together.',
  },
  'Platform Engineering': {
    slug: 'platform-engineering',
    blurb:
      'Building the internal platform other engineers ship on — paved paths, developer experience, and where to draw the abstraction.',
  },
  'Cloud & Infrastructure': {
    slug: 'cloud-infrastructure',
    blurb:
      'Cloud architecture and the bill that comes with it — networking, compute, infrastructure as code, migrations and cost.',
  },
  Databases: {
    slug: 'databases',
    blurb:
      'Choosing, modelling and operating databases — schema design, indexing, query performance, migrations and scaling.',
  },

  // Product & Design
  'Product Management': {
    slug: 'product-management',
    blurb:
      'Product craft — discovery, prioritisation, working with engineering, and telling the difference between output and outcome.',
  },
  'UX/UI Design': {
    slug: 'ux-ui-design',
    blurb:
      'Interface and experience design — research, flows, design systems, and defending the decisions in a review.',
  },

  // Leadership & Career
  'Engineering Management': {
    slug: 'engineering-management',
    blurb:
      'Leading engineers — one-on-ones, performance, hiring, team health, and the shift from writing the code to enabling it.',
  },
  'Tech Lead': {
    slug: 'tech-lead',
    blurb:
      'Technical leadership without the title change — direction, decisions, review culture, and keeping a team unblocked.',
  },
  'Staff+/IC Growth': {
    slug: 'staff-ic-growth',
    blurb:
      'The senior-to-staff transition — scope, influence without authority, technical strategy, and what promotion committees look for.',
  },
  'Career Growth': {
    slug: 'career-growth',
    blurb:
      'Where to go next — levelling up, switching tracks, finding the right role, and building a case for the step you want.',
  },
  'Interview Prep': {
    slug: 'interview-prep',
    blurb:
      'Preparing for real loops — coding, system design and behavioural rounds, practised under something close to real conditions.',
  },
  'Compensation & Negotiation': {
    slug: 'compensation-negotiation',
    blurb:
      'Offers and what they are actually worth — levelling, equity, benchmarking, and negotiating without burning the relationship.',
  },
  'Relocation & Working Abroad': {
    slug: 'relocation-working-abroad',
    blurb:
      'Moving countries for work — visas, job hunting from abroad, remote versus relocation, and settling into a new market.',
  },
  'Project Management': {
    slug: 'project-management',
    blurb:
      'Delivery — planning, scope, dependencies, risk, and keeping stakeholders honest about all four.',
  },

  // Business & Communication
  'Entrepreneurship & Startups': {
    slug: 'entrepreneurship-startups',
    blurb:
      'Starting and building — validation, early product, fundraising, and the parts of company-building nobody warns you about.',
  },
  'Freelancing & Consulting': {
    slug: 'freelancing-consulting',
    blurb:
      'Working for yourself — finding clients, pricing, contracts, positioning, and making the income less lumpy.',
  },
  'Developer Relations': {
    slug: 'developer-relations',
    blurb:
      'Advocacy and community — content, talks, developer experience, and measuring impact that resists easy measurement.',
  },
  'Technical Writing': {
    slug: 'technical-writing',
    blurb:
      'Documentation and technical communication — structure, audience, tone, and writing that people actually finish.',
  },
  'Marketing & Growth': {
    slug: 'marketing-growth',
    blurb:
      'Reaching the people who need the product — positioning, channels, content, funnels and honest measurement.',
  },
  'People & HR': {
    slug: 'people-hr',
    blurb:
      'The people side — hiring processes, onboarding, culture, feedback, and the operational work of growing a team.',
  },
}

/** Every tag, in taxonomy order. */
export const ALL_TAGS = Object.keys(TAG_PAGES) as MentorTag[]

const BY_SLUG = new Map<string, MentorTag>(
  ALL_TAGS.map((tag) => [TAG_PAGES[tag].slug, tag] as const)
)

/** Resolve a /mentors/<slug> segment back to its tag, or null if unknown. */
export function tagBySlug(slug: string): MentorTag | null {
  return BY_SLUG.get(slug) ?? null
}

/** The public URL path for a tag's landing page. */
export function tagPath(tag: MentorTag): string {
  return `/mentors/${TAG_PAGES[tag].slug}`
}

/**
 * Does this string name a tag with a landing page?
 *
 * Needed because tags reach the UI as plain `string`: `mentor.tags` comes from
 * the API, and `FilterCategory.tags` is declared `string[]`. A tag that drifted
 * out of the taxonomy (a rename, or a DB row the union no longer lists — the
 * failure mode D30 was written about) must render as plain text, never as a
 * link to a page that would 404.
 */
export function isKnownTag(tag: string): tag is MentorTag {
  return Object.prototype.hasOwnProperty.call(TAG_PAGES, tag)
}

/**
 * The catalog's six taxonomy groups, narrowed to tags that actually have a
 * landing page. Single source for every "browse by topic" block, so the type
 * guard lives here rather than being repeated at each call site.
 */
export const TAG_CATEGORIES: { label: string; tags: MentorTag[] }[] = filters.categories.map(
  (category) => ({
    label: category.label,
    tags: category.tags.filter(isKnownTag),
  })
)
