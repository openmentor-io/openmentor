import Head from 'next/head'
// next/compat/router returns null instead of throwing when no router is
// mounted — which is the case while Next statically prerenders /404.
import { useRouter } from 'next/compat/router'
import constants from '@/config/constants'
import seo from '@/config/seo'

interface MetaHeaderProps {
  customTitle?: string
  /**
   * Page-specific description, used VERBATIM (not concatenated with the
   * generic site description — scrapers truncate at ~160 chars, so the
   * specific text must come first and stand alone).
   */
  customDescription?: string
  /** Absolute URL of the social image (1200×630). Defaults to the banner. */
  customImage?: string | null
  /** Alt text for the social image. */
  imageAlt?: string
  /**
   * Absolute canonical URL. Optional — defaults to the current path on
   * BASE_URL. Pass it only when the canonical differs from the request path
   * or when it is computed server-side (mentor profiles).
   */
  canonicalUrl?: string
  /** Open Graph object type (mentor profiles are 'profile'). */
  ogType?: 'website' | 'profile'
  /** Keep the page out of the index (portal/admin/transactional pages). */
  noIndex?: boolean
}

/**
 * Social/SEO meta tags. Notes for future edits:
 * - Open Graph tags MUST use `property=` (not `name=`) — Facebook, LinkedIn,
 *   Slack, WhatsApp and Telegram ignore og:* tags declared via `name=`.
 * - Twitter/X tags use `name=`. `summary_large_image` is correct for our
 *   1200×630 images (both the banner and the /api/og/mentor cards).
 * - This component renders its OWN next/head — place it as a SIBLING of the
 *   page's <Head>, never inside one. next/head collects its children in a
 *   separate render pass with no context providers, so a MetaHeader nested in
 *   <Head> gets no router and silently loses its canonical URL.
 */
export default function MetaHeader({
  customTitle,
  customDescription,
  customImage,
  imageAlt,
  canonicalUrl,
  ogType = 'website',
  noIndex = false,
}: MetaHeaderProps) {
  const router = useRouter()

  // Social titles stay short: og:site_name already carries the brand, and
  // scrapers truncate around 65 chars — no "| OpenMentor.io" suffix here
  // (the <title> tag, set by each page, keeps the full suffixed form).
  const page_title = customTitle || seo.title
  const page_description = customDescription || seo.description
  const page_image = customImage ?? seo.imageUrl
  const page_image_alt = imageAlt ?? 'OpenMentor.io — an open community of tech mentors'

  // Query strings and hashes never identify a distinct page here (filters are
  // client state, ?utm_* is tracking), so they are stripped from the default.
  // Statically prerendered pages (/404) have no router and get no canonical —
  // correct, since they are not a canonical anything.
  const path = router?.asPath.split(/[?#]/)[0]
  const canonical = canonicalUrl ?? (path ? constants.BASE_URL + path.replace(/^\/+/, '') : null)

  return (
    <Head>
      <meta name="description" content={page_description} />
      {canonical && <link rel="canonical" href={canonical} />}

      {/* max-image-preview:large opts into large thumbnails in Search and
          Discover; without it Google shows a small one or none at all. */}
      <meta
        name="robots"
        content={
          noIndex
            ? 'noindex,follow'
            : 'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1'
        }
      />

      <meta property="og:site_name" content="OpenMentor.io" />
      <meta property="og:type" content={ogType} />
      <meta property="og:locale" content="en_US" />
      <meta property="og:title" content={page_title} />
      <meta property="og:description" content={page_description} />
      {canonical && <meta property="og:url" content={canonical} />}
      <meta property="og:image" content={page_image} />
      <meta property="og:image:width" content="1200" />
      <meta property="og:image:height" content="630" />
      <meta property="og:image:alt" content={page_image_alt} />

      <meta name="twitter:card" content="summary_large_image" />
      <meta name="twitter:title" content={page_title} />
      <meta name="twitter:description" content={page_description} />
      <meta name="twitter:image" content={page_image} />
      <meta name="twitter:image:alt" content={page_image_alt} />
    </Head>
  )
}
