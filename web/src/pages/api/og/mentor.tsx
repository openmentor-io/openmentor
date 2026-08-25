/**
 * Dynamic social-share card for mentor profiles (Open Graph image).
 *
 * GET /api/og/mentor?slug=<mentor-slug>[&v=<cache-buster>]
 *
 * Renders a branded 1200×630 PNG that mirrors the catalog card: the mentor's
 * deterministic pastel gradient, the photo in the arch-frame treatment (or
 * the initials circle), name, role and meta chips. Mentor data is fetched
 * from the Go API by slug — nothing user-controlled is drawn, so cards can't
 * be spoofed via query params. Any failure falls back to a redirect to the
 * static site banner: a scraper must always get *an* image.
 *
 * The endpoint is bounded on four axes (C8), because one request costs two
 * upstream fetches plus a 1200x630 satori render and it is reachable
 * unauthenticated by anyone who can type a URL:
 *   - method: GET/HEAD only, so a POST flood cannot drive renders;
 *   - slug: rejected against the username grammar before any upstream call;
 *   - concurrency: at most MAX_CONCURRENT_RENDERS in flight, the rest shed to
 *     the banner rather than queueing behind the event loop;
 *   - caching: rendered PNGs are held in-process, so a `?v=` sweep re-serves
 *     bytes instead of re-rendering.
 *
 * Runs in the regular Node runtime: next/og ships a Node build, and the edge
 * runtime variant is mis-bundled by Turbopack for Pages Router API routes
 * (broken `__import_unsupported` zlib shim, Next 16.0.10 — in dev AND build).
 * Fonts are OFL-licensed static TTFs vendored in src/assets/og-fonts (satori
 * cannot consume the woff2 files next/font serves); they are read from disk
 * and shipped in the standalone build via outputFileTracingIncludes.
 */
import { promises as fs } from 'fs'
import path from 'path'
import type { NextApiRequest, NextApiResponse } from 'next'
import { ImageResponse } from 'next/og'
import logger from '@/lib/logger'
import { withObservability } from '@/lib/with-observability'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import {
  MENTOR_INITIALS_HEX,
  MENTOR_PASTEL_GRAD_HEX,
  mentorInitialsIndex,
  mentorPastelIndex,
} from '@/lib/mentor-pastel'
import { fixedAmount, isFree } from '@/lib/price'

const WIDTH = 1200
const HEIGHT = 630

// Brand palette (source of truth: web/src/styles/brand-tokens.css +
// tailwind.config.js) — hex-inlined because Tailwind can't style satori.
const INK = '#161A20'
const INK_MUTE = '#4A5160'
const NAVY = '#132A52'
const MINT_INK = '#0E7A70'

const FONT_DIR = path.join(process.cwd(), 'src', 'assets', 'og-fonts')

/** Font buffers, loaded once per process and reused across requests. */
let fontsPromise: Promise<{ schibsted: Buffer; archivo: Buffer; inter: Buffer }> | null = null

function loadFonts(): NonNullable<typeof fontsPromise> {
  if (!fontsPromise) {
    fontsPromise = Promise.all([
      fs.readFile(path.join(FONT_DIR, 'schibsted-grotesk-700.ttf')),
      fs.readFile(path.join(FONT_DIR, 'archivo-800.ttf')),
      fs.readFile(path.join(FONT_DIR, 'inter-500.ttf')),
    ]).then(([schibsted, archivo, inter]) => ({ schibsted, archivo, inter }))
  }
  return fontsPromise
}

interface OgMentor {
  mentorId: string
  name: string
  job: string
  workplace: string
  experience: string
  price: string
  slug: string
  updatedAt?: string
}

/** Fetch the mentor by slug straight from the Go API (raw fetch, no client). */
async function fetchMentor(slug: string): Promise<OgMentor | null> {
  const baseURL = process.env.NEXT_PUBLIC_GO_API_URL || 'http://localhost:8081'
  const token = process.env.GO_API_INTERNAL_TOKEN || ''

  const res = await fetch(`${baseURL}/api/v1/internal/mentors?slug=${encodeURIComponent(slug)}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'x-internal-mentors-api-auth-token': token,
    },
    body: JSON.stringify({}),
    signal: AbortSignal.timeout(5000),
  })
  if (!res.ok) {
    return null
  }
  return (await res.json()) as OgMentor
}

/** Fetch the mentor photo bytes (keyed by the immutable mentor UUID). */
async function fetchPhoto(mentorId: string, updatedAt?: string): Promise<ArrayBuffer | null> {
  try {
    const url = imageLoader({
      src: mentorId,
      quality: 'large',
      version: updatedAtToVersion(updatedAt),
    })
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) })
    if (!res.ok || !(res.headers.get('content-type') || '').startsWith('image/')) {
      return null
    }
    return await res.arrayBuffer()
  } catch {
    return null
  }
}

/** "2-5" -> "2–5Y EXP", "10+" -> "10+Y EXP" (mirrors the catalog meta row). */
function experienceLabel(experience: string): string {
  return `${experience.replace('-', '–')}Y EXP`
}

/** "FREE" / "$150" / "NEGOTIABLE" chip text + color (mirrors PriceBadge). */
function priceChip(price: string): { label: string; color: string } {
  if (isFree(price)) {
    return { label: 'FREE', color: MINT_INK }
  }
  const amount = fixedAmount(price)
  if (amount !== null) {
    // Canonical spelling, matching the card and the profile page ("$1000",
    // no separator) — the social unfurl and the page it links to must not
    // disagree on the price.
    return { label: `$${amount}`, color: NAVY }
  }
  // Unlike the catalog card, a value predating the grammar is NOT rendered
  // verbatim here but relabelled NEGOTIABLE: the raster is rendered once and
  // cached, so an odd legacy string would be baked into the share card with
  // no quick fix, and mentors_price_chk makes the case unreachable for stored
  // data anyway.
  return { label: 'NEGOTIABLE', color: INK_MUTE }
}

/** First letters of the first two name words (same rule as MentorPortrait). */
function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('')
}

/**
 * Concurrent renders allowed in flight. satori + resvg are synchronous CPU on
 * the Node event loop, so unbounded concurrency does not "queue" — it stalls
 * every other request the frontend is serving, including the ones the mentor
 * pages need. Excess requests are shed to the banner: a scraper that gets the
 * generic image is a worse card, an unresponsive frontend is an outage.
 */
const MAX_CONCURRENT_RENDERS = 3
let activeRenders = 0

/**
 * Rendered cards, keyed by slug alone.
 *
 * Deliberately NOT keyed by `?v=`: `v` exists to bust the CDN when a profile
 * changes, and keying on it would let an arbitrary `v` sweep miss the cache on
 * every request — the exact multiplication this is here to stop. Keying by slug
 * means such a sweep costs one render per slug per TTL, at the price of a card
 * lagging a profile edit by up to the TTL. That lag is invisible next to the
 * `s-maxage=86400` the response already carries.
 *
 * Bounded by entry count rather than bytes: a 1200x630 PNG is a few hundred KB,
 * so this is single-digit MB at the ceiling.
 */
const CARD_CACHE_TTL_MS = 10 * 60 * 1000
const CARD_CACHE_MAX_ENTRIES = 16
const cardCache = new Map<string, { png: Buffer; expiresAt: number }>()

function cachedCard(slug: string): Buffer | null {
  const hit = cardCache.get(slug)
  if (!hit) return null
  if (hit.expiresAt <= Date.now()) {
    cardCache.delete(slug)
    return null
  }
  // Re-insert so Map iteration order stays least-recently-used first.
  cardCache.delete(slug)
  cardCache.set(slug, hit)
  return hit.png
}

function cacheCard(slug: string, png: Buffer): void {
  cardCache.delete(slug)
  cardCache.set(slug, { png, expiresAt: Date.now() + CARD_CACHE_TTL_MS })
  while (cardCache.size > CARD_CACHE_MAX_ENTRIES) {
    const oldest = cardCache.keys().next()
    if (oldest.done) break
    cardCache.delete(oldest.value)
  }
}

/** Test seam: the cache is process-global and would leak between test cases. */
export function __resetCardCache(): void {
  cardCache.clear()
  activeRenders = 0
}

/**
 * The cache-buster the mentor page appends: `updatedAtToVersion`, i.e. epoch
 * seconds. Anything else is not from us, and honouring it would only mint CDN
 * cache keys, so it is refused before any upstream work happens.
 */
function isVersionParamValid(value: string | string[] | undefined): boolean {
  if (value === undefined) return true
  if (Array.isArray(value)) return false
  return /^[0-9]{1,12}$/.test(value)
}

/**
 * A bound on the slug, deliberately looser than the username grammar
 * (`api/pkg/slug`, D29: `^[a-z0-9]+(-[a-z0-9]+)*$`, 3-40 chars).
 *
 * The grammar would be the obvious check and it is the wrong one here: it
 * postdates the getmentor.dev import, and at least one live profile has a
 * leading-hyphen slug that predates it. Gating on the grammar would replace a
 * working card with the generic banner for those mentors. What this needs to
 * stop is unbounded input reaching the upstream fetch — a traversal attempt, a
 * multi-kilobyte string, an encoded null — so charset and length are enough,
 * with a 64-char ceiling comfortably past anything the grammar can produce.
 */
const SLUG_BOUND = /^[a-z0-9-]{1,64}$/

function sendCard(res: NextApiResponse, png: Buffer): void {
  res.setHeader('Content-Type', 'image/png')
  // Cards change when the profile does — the page busts via ?v=.
  res.setHeader(
    'Cache-Control',
    'public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800'
  )
  res.status(200).send(png)
}

async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  // A transient or refused card must not be cached as this mentor's image, so
  // the banner redirect is explicitly uncacheable.
  const fallback = (): void => {
    res.setHeader('Cache-Control', 'no-store')
    res.redirect(302, '/images/banner.png')
  }

  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.setHeader('Allow', 'GET, HEAD')
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  // A repeated `?slug=` is refused rather than resolved to the first value: no
  // legitimate caller sends one, and accepting it would give every mentor an
  // unbounded family of URLs that render the same card.
  const slugParam = req.query.slug
  const slug = Array.isArray(slugParam) ? undefined : slugParam?.trim()

  // Both checks run before any upstream call or render, so a probe costs a
  // regex rather than two HTTP round trips.
  if (!slug || !SLUG_BOUND.test(slug) || !isVersionParamValid(req.query.v)) {
    fallback()
    return
  }

  const cached = cachedCard(slug)
  if (cached) {
    sendCard(res, cached)
    return
  }

  if (activeRenders >= MAX_CONCURRENT_RENDERS) {
    logger.warn('OG card render shed at capacity, redirecting to banner', {
      slug,
      activeRenders,
    })
    fallback()
    return
  }

  activeRenders += 1
  try {
    const mentor = await fetchMentor(slug)
    if (!mentor) {
      fallback()
      return
    }

    const [photo, fonts] = await Promise.all([
      fetchPhoto(mentor.mentorId, mentor.updatedAt),
      loadFonts(),
    ])
    const [base, deep] = MENTOR_PASTEL_GRAD_HEX[mentorPastelIndex(mentor.slug)]
    const initialsFill = MENTOR_INITIALS_HEX[mentorInitialsIndex(mentor.slug)]
    const price = priceChip(mentor.price)

    // Long names step down so they never collide with the photo column.
    const nameSize = mentor.name.length > 26 ? 52 : mentor.name.length > 18 ? 62 : 72

    const chip = (label: string, color: string): JSX.Element => (
      <div
        style={{
          display: 'flex',
          backgroundColor: 'rgba(255,255,255,0.9)',
          borderRadius: 12,
          padding: '10px 18px',
          fontFamily: 'Archivo',
          fontSize: 22,
          letterSpacing: 1,
          color,
        }}
      >
        {label}
      </div>
    )

    const image = new ImageResponse(
      (
        <div
          style={{
            width: '100%',
            height: '100%',
            display: 'flex',
            flexDirection: 'column',
            padding: '56px 64px 0',
            backgroundImage: `linear-gradient(160deg, ${base} 0%, ${deep} 100%)`,
          }}
        >
          {/* Wordmark row (CSS logomark echo: ring + mint node) */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
            <div style={{ display: 'flex', position: 'relative', width: 44, height: 44 }}>
              <div
                style={{
                  width: 40,
                  height: 40,
                  margin: 2,
                  borderRadius: 999,
                  border: `8px solid ${NAVY}`,
                }}
              />
              <div
                style={{
                  position: 'absolute',
                  right: 0,
                  top: 0,
                  width: 12,
                  height: 12,
                  borderRadius: 999,
                  backgroundColor: '#17C3B2',
                }}
              />
            </div>
            <div style={{ fontFamily: 'Archivo', fontSize: 28, color: NAVY, letterSpacing: -0.5 }}>
              OPENMENTOR.IO
            </div>
            <div
              style={{
                fontFamily: 'Archivo',
                fontSize: 20,
                color: INK_MUTE,
                letterSpacing: 1,
                marginLeft: 'auto',
              }}
            >
              1:1 MENTORSHIP · 0% COMMISSION
            </div>
          </div>

          {/* Main row: text column + photo column (photo bottom-bleeds) */}
          <div
            style={{
              display: 'flex',
              flex: 1,
              alignItems: 'flex-end',
              justifyContent: 'space-between',
              marginTop: 20,
            }}
          >
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                maxWidth: 690,
                paddingBottom: 64,
              }}
            >
              <div
                style={{
                  fontFamily: 'Schibsted',
                  fontSize: nameSize,
                  lineHeight: 1.05,
                  color: INK,
                  letterSpacing: -2,
                }}
              >
                {mentor.name}
              </div>
              <div
                style={{
                  fontFamily: 'Inter',
                  fontSize: 30,
                  lineHeight: 1.3,
                  color: INK_MUTE,
                  marginTop: 16,
                }}
              >
                {`${mentor.job} · ${mentor.workplace}`}
              </div>
              <div style={{ display: 'flex', gap: 12, marginTop: 32 }}>
                {chip(experienceLabel(mentor.experience), NAVY)}
                {chip(price.label, price.color)}
              </div>
            </div>

            {photo ? (
              // Arch-frame treatment: rounded-top tile, white keyline,
              // bottom-anchored like the catalog card.
              <div
                style={{
                  display: 'flex',
                  width: 340,
                  height: 430,
                  borderRadius: '28px 28px 0 0',
                  border: '6px solid rgba(255,255,255,0.75)',
                  borderBottom: 'none',
                  overflow: 'hidden',
                  flexShrink: 0,
                }}
              >
                {/* eslint-disable-next-line @next/next/no-img-element -- satori
                    renders plain elements; next/image does not exist here */}
                <img
                  src={photo as unknown as string}
                  alt=""
                  width={328}
                  height={430}
                  style={{ objectFit: 'cover', objectPosition: '50% 20%' }}
                />
              </div>
            ) : (
              // Initials fallback (no photo in storage).
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  alignSelf: 'center',
                  width: 260,
                  height: 260,
                  marginRight: 30,
                  borderRadius: 999,
                  backgroundColor: initialsFill,
                  color: '#FFFFFF',
                  fontFamily: 'Schibsted',
                  fontSize: 96,
                }}
              >
                {initials(mentor.name)}
              </div>
            )}
          </div>
        </div>
      ),
      {
        width: WIDTH,
        height: HEIGHT,
        fonts: [
          { name: 'Schibsted', data: fonts.schibsted, weight: 700, style: 'normal' },
          { name: 'Archivo', data: fonts.archivo, weight: 800, style: 'normal' },
          { name: 'Inter', data: fonts.inter, weight: 500, style: 'normal' },
        ],
      }
    )

    const png = Buffer.from(await image.arrayBuffer())
    cacheCard(slug, png)
    sendCard(res, png)
  } catch (error) {
    // Never break a scraper — but do leave a trace for us.
    logger.error('OG card render failed, redirecting to banner', {
      slug,
      error: error instanceof Error ? error.message : String(error),
    })
    fallback()
  } finally {
    activeRenders -= 1
  }
}

export default withObservability('/api/og/mentor', handler)
