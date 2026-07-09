import type { SEOConfig } from '@/types'

const seo: SEOConfig = {
  title: 'OpenMentor — an open community of tech mentors',
  description:
    'OpenMentor is an open community of tech mentors ready to share their experience and knowledge in one-on-one conversations.',
  imageUrl: 'https://openmentor.io/images/banner.png',
  domain: process.env.DOMAIN || 'https://openmentor.io',
}

export default seo
