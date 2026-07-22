import type { SEOConfig } from '@/types'

const seo: SEOConfig = {
  title: 'OpenMentor — an open community of tech mentors',
  description:
    'OpenMentor is an open community of tech mentors sharing their experience in one-on-one conversations. Free to browse, zero commission — many mentors are free.',
  imageUrl: 'https://openmentor.io/images/banner.png',
  domain: process.env.DOMAIN || 'https://openmentor.io',
}

export default seo
