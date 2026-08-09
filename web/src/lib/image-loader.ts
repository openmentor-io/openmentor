export type ImageSize = 'full' | 'large' | 'small'

/**
 * The one object a mentor photo is stored as (api/pkg/s3storage, audit C5).
 *
 * `full`/`large`/`small` were never variants: the API uploaded the SAME bytes
 * under all three keys, so picking a "size" chose between three copies of one
 * image at three times the storage and egress. The API now writes only this
 * key, so every reader has to resolve to it.
 *
 * The key layout is unchanged, which is what keeps existing photos working —
 * every mentor who uploaded before the change still has all three objects, and
 * `full` is one of them. When real thumbnails exist, the other sizes come back
 * as different bytes under the same keys and the size argument starts meaning
 * something again.
 */
const CANONICAL_SIZE: ImageSize = 'full'

interface ImageLoaderParams {
  src: string
  /**
   * Requested rendered width, and the size the caller would like. Both are
   * accepted (this matches the next/image loader signature, and the call sites
   * describe what they need) but neither selects an object today — see
   * CANONICAL_SIZE.
   */
  width?: number
  quality?: ImageSize | number
  version?: string | number
}

const STORAGE_DOMAIN =
  process.env.NEXT_PUBLIC_CDN_ENDPOINT || process.env.NEXT_PUBLIC_S3_STORAGE_ENDPOINT

const STORAGE_BUCKET = process.env.NEXT_PUBLIC_S3_STORAGE_BUCKET || 'mentor-images'

export function imageLoader({ src, version }: ImageLoaderParams): string {
  // Local development: a localhost endpoint (e.g. the dev server itself
  // serving fixture images) has no TLS.
  const scheme =
    STORAGE_DOMAIN?.startsWith('localhost') || STORAGE_DOMAIN?.startsWith('127.')
      ? 'http://'
      : 'https://'
  const url =
    scheme + STORAGE_DOMAIN + (process.env.NEXT_PUBLIC_CDN_ENDPOINT ? '' : '/' + STORAGE_BUCKET)

  const baseUrl = `${url}/${src}/${CANONICAL_SIZE}`
  return version !== undefined ? `${baseUrl}?v=${version}` : baseUrl
}

export function updatedAtToVersion(updatedAt: string | undefined): number | undefined {
  if (!updatedAt) return undefined
  const ms = new Date(updatedAt).getTime()
  return isNaN(ms) ? undefined : Math.floor(ms / 1000)
}
