import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import {
  MAX_IMAGE_FILE_BYTES,
  MAX_IMAGE_FILE_LABEL,
  MAX_IMAGE_REQUEST_BODY,
} from '@/config/uploads'

// Every route that forwards a photo. Next's bodyParser config has to be a
// static literal, so these files cannot import MAX_IMAGE_REQUEST_BODY and the
// only thing keeping them honest is this test.
const PHOTO_ROUTES = [
  'src/pages/api/register-mentor.ts',
  'src/pages/api/mentor/profile/picture.ts',
  'src/pages/api/admin/mentors/[id]/picture.ts',
]

function parseBytes(limit: string): number {
  const match = /^(\d+)(kb|mb)$/.exec(limit)
  if (!match) throw new Error(`unsupported size literal: ${limit}`)
  return Number(match[1]) * (match[2] === 'mb' ? 1024 * 1024 : 1024)
}

describe('the photo upload size contract', () => {
  // The bug this guards: a photo is validated as a RAW file and transported as
  // base64 inside JSON. Sizing the body limit as if it were the file limit made
  // the largest photo that actually fit ~7.8 MB, while the form promised 10 MB —
  // so an 8-10 MB photo passed the picker and then 413'd at a body reader.
  it('carries a photo at the advertised limit once base64-encoded', () => {
    const encoded = 4 * Math.ceil(MAX_IMAGE_FILE_BYTES / 3)
    const dataUrlPrefix = 'data:image/jpeg;base64,'.length
    // Every text field of the registration form at its server-side `max=`, in
    // the worst encoding JSON allows (12 bytes per rune, astral surrogate
    // pairs) — the same 286,628 bytes api/internal/middleware budgets for.
    const formFields = 286_628

    expect(parseBytes(MAX_IMAGE_REQUEST_BODY)).toBeGreaterThanOrEqual(
      encoded + dataUrlPrefix + formFields
    )
  })

  it('is the same limit the Go body cap enforces one hop later', () => {
    // api/internal/middleware.MaxImageBodyBytes. The proxy must not accept what
    // the API will refuse, nor refuse what the API would accept.
    expect(parseBytes(MAX_IMAGE_REQUEST_BODY)).toBe(14 * 1024 * 1024)
  })

  it('is declared by every route that forwards a photo', () => {
    for (const route of PHOTO_ROUTES) {
      const source = readFileSync(join(process.cwd(), route), 'utf8')
      expect(source).toContain(`sizeLimit: '${MAX_IMAGE_REQUEST_BODY}'`)
    }
  })

  it('advertises the file limit it actually enforces', () => {
    expect(MAX_IMAGE_FILE_LABEL).toBe(`${MAX_IMAGE_FILE_BYTES / 1024 / 1024} MB`)
  })
})
