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
  /** Absolute canonical URL — emits og:url + <link rel="canonical">. */
  canonicalUrl?: string
  /** Open Graph object type (mentor profiles are 'profile'). */
  ogType?: 'website' | 'profile'
}

/**
 * Social/SEO meta tags. Notes for future edits:
 * - Open Graph tags MUST use `property=` (not `name=`) — Facebook, LinkedIn,
 *   Slack, WhatsApp and Telegram ignore og:* tags declared via `name=`.
 * - Twitter/X tags use `name=`. `summary_large_image` is correct for our
 *   1200×630 images (both the banner and the /api/og/mentor cards).
 * - Rendered inside next/head — keep every tag a direct child (no wrappers).
 */
export default function MetaHeader({
  customTitle,
  customDescription,
  customImage,
  imageAlt,
  canonicalUrl,
  ogType = 'website',
}: MetaHeaderProps) {
  // Social titles stay short: og:site_name already carries the brand, and
  // scrapers truncate around 65 chars — no "| OpenMentor …" suffix here
  // (the <title> tag, set by each page, keeps the full suffixed form).
  const page_title = customTitle || seo.title
  const page_description = customDescription || seo.description
  const page_image = customImage ?? seo.imageUrl
  const page_image_alt = imageAlt ?? 'OpenMentor — an open community of tech mentors'

  return (
    <>
      <meta name="description" content={page_description} />
      {canonicalUrl && <link rel="canonical" href={canonicalUrl} />}

      <meta property="og:site_name" content="OpenMentor" />
      <meta property="og:type" content={ogType} />
      <meta property="og:title" content={page_title} />
      <meta property="og:description" content={page_description} />
      {canonicalUrl && <meta property="og:url" content={canonicalUrl} />}
      <meta property="og:image" content={page_image} />
      <meta property="og:image:width" content="1200" />
      <meta property="og:image:height" content="630" />
      <meta property="og:image:alt" content={page_image_alt} />

      <meta name="twitter:card" content="summary_large_image" />
      <meta name="twitter:title" content={page_title} />
      <meta name="twitter:description" content={page_description} />
      <meta name="twitter:image" content={page_image} />
      <meta name="twitter:image:alt" content={page_image_alt} />
    </>
  )
}
