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
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import {
  MENTOR_INITIALS_HEX,
  MENTOR_PASTEL_GRAD_HEX,
  mentorInitialsIndex,
  mentorPastelIndex,
} from '@/lib/mentor-pastel'
import { isPriceFree, parsePriceAmount } from '@/config/filters'

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

/** Fetch the mentor photo bytes; null when there is no usable photo. */
async function fetchPhoto(slug: string, updatedAt?: string): Promise<ArrayBuffer | null> {
  try {
    const url = imageLoader({ src: slug, quality: 'large', version: updatedAtToVersion(updatedAt) })
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
  if (isPriceFree(price)) {
    return { label: 'FREE', color: MINT_INK }
  }
  const amount = parsePriceAmount(price)
  if (amount !== null) {
    return { label: `$${amount.toLocaleString('en-US')}`, color: NAVY }
  }
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

export default async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  const fallback = (): void => {
    res.redirect(302, '/images/banner.png')
  }

  try {
    const slugParam = req.query.slug
    const slug = (Array.isArray(slugParam) ? slugParam[0] : slugParam)?.trim()
    if (!slug) {
      fallback()
      return
    }

    const mentor = await fetchMentor(slug)
    if (!mentor) {
      fallback()
      return
    }

    const [photo, fonts] = await Promise.all([
      fetchPhoto(mentor.slug, mentor.updatedAt),
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
    res.setHeader('Content-Type', 'image/png')
    // Cards change when the profile does — the page busts via ?v=.
    res.setHeader(
      'Cache-Control',
      'public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800'
    )
    res.status(200).send(png)
  } catch (error) {
    // Never break a scraper — but do leave a trace for us.
    logger.error('OG card render failed, redirecting to banner', {
      slug: req.query.slug,
      error: error instanceof Error ? error.message : String(error),
    })
    fallback()
  }
}
