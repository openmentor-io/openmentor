import type { GetServerSideProps } from 'next'
import { getAllMentors } from '@/server/mentors-data'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import constants from '@/config/constants'

const baseUrl = constants.BASE_URL

// SECURITY (L7): escape XML metacharacters before interpolating into <loc>.
// Slugs are currently constrained, but an '&' or '<' (from a backend change or
// a lenient admin edit) would otherwise corrupt or inject into the document.
function escapeXml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

function sitemapItem(path: string): string {
  return `
        <url>
        <loc>${escapeXml(baseUrl + path)}</loc>
        <lastmod>${new Date().toISOString()}</lastmod>
        <changefreq>weekly</changefreq>
        <priority>0.5</priority>
        </url>
    `
}

const _getServerSideProps: GetServerSideProps = async ({ res }) => {
  const allMentors = await getAllMentors({ onlyVisible: true })

  const staticPages = [
    { page: '' },
    { page: 'about' },
    { page: 'faq' },
    { page: 'bementor' },
    { page: 'donate' },
  ]

  const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
        <urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
        ${staticPages.map((s) => sitemapItem(s.page)).join('')}
        ${allMentors.map((m) => sitemapItem('mentor/' + m.slug)).join('')}
        </urlset>
    `

  res.setHeader('Content-Type', 'text/xml')
  res.write(sitemap)
  res.end()

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'sitemap')

export default function Sitemap(): null {
  return null
}
