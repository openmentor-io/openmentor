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

// Date-only W3C Datetime form (YYYY-MM-DD) — we only know the day a mentor
// changed, not the second, so don't claim precision we don't have.
function toLastmodDate(updatedAt: string | undefined): string | undefined {
  if (!updatedAt) return undefined
  const ms = new Date(updatedAt).getTime()
  if (isNaN(ms)) return undefined
  return new Date(ms).toISOString().slice(0, 10)
}

function sitemapItem(path: string, updatedAt?: string): string {
  const lastmod = toLastmodDate(updatedAt)
  return `
        <url>
        <loc>${escapeXml(baseUrl + path)}</loc>
        ${lastmod ? `<lastmod>${lastmod}</lastmod>` : ''}
        </url>
    `
}

const _getServerSideProps: GetServerSideProps = async ({ res }) => {
  const allMentors = await getAllMentors({ onlyVisible: true })

  const staticPages = [
    { page: '' },
    { page: 'mentors' },
    { page: 'about' },
    { page: 'faq' },
    { page: 'bementor' },
    { page: 'donate' },
    { page: 'privacy' },
    { page: 'terms' },
  ]

  const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
        <urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
        ${staticPages.map((s) => sitemapItem(s.page)).join('')}
        ${allMentors.map((m) => sitemapItem('mentor/' + m.slug, m.updatedAt)).join('')}
        </urlset>
    `

  res.setHeader('Content-Type', 'text/xml')
  // avoid re-fetching the mentor list from the Go API on every crawler hit
  res.setHeader('Cache-Control', 'public, s-maxage=3600, stale-while-revalidate=86400')
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
