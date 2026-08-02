import type { SEOConfig } from '@/types'

const seo: SEOConfig = {
  title: 'OpenMentor.io — an open community of tech mentors',
  titleSuffix: 'OpenMentor.io',
  description:
    'OpenMentor is an open community of tech mentors sharing their experience in one-on-one conversations. Free to browse, zero commission — many mentors are free.',
  imageUrl: 'https://openmentor.io/images/banner.png',
}

/**
 * Page <title>. Kept short — SERPs truncate around 60 characters, so the
 * brand suffix must not eat the page-specific words. ".io" is part of the
 * brand name and stays in every title.
 */
export function pageTitle(name: string): string {
  return `${name} | ${seo.titleSuffix}`
}

export default seo
