/**
 * The loader builds the URL a mentor photo is fetched from, so it is the one
 * place that has to agree with what api/pkg/s3storage actually writes. Since
 * audit C5 that is a single object per mentor — the loader must never point at
 * a size the API no longer stores.
 */

const MENTOR_UUID = '11111111-2222-3333-4444-555555555555'

// The module reads the storage env once, at import, so each case imports it
// fresh with the env it needs.
async function loadLoader(env: Record<string, string | undefined>) {
  jest.resetModules()
  for (const key of ['NEXT_PUBLIC_CDN_ENDPOINT', 'NEXT_PUBLIC_S3_STORAGE_ENDPOINT']) {
    delete process.env[key]
  }
  Object.assign(process.env, env)
  return import('@/lib/image-loader')
}

describe('imageLoader', () => {
  const storageEnv = {
    NEXT_PUBLIC_S3_STORAGE_ENDPOINT: 'storage.example.com',
    NEXT_PUBLIC_S3_STORAGE_BUCKET: 'mentor-images',
  }

  it('resolves every requested size to the one stored object', async () => {
    const { imageLoader } = await loadLoader(storageEnv)
    const canonical = `https://storage.example.com/mentor-images/${MENTOR_UUID}/full`

    // full/large/small were byte-identical copies; the API stores only 'full'
    // now, so asking for a thumbnail must not produce a key nothing writes.
    for (const quality of ['small', 'large', 'full'] as const) {
      expect(imageLoader({ src: MENTOR_UUID, quality })).toBe(canonical)
    }

    // Same for the width-derived sizes the loader used to pick from.
    for (const width of [16, 36, 320, 900, 1200]) {
      expect(imageLoader({ src: MENTOR_UUID, width })).toBe(canonical)
    }
  })

  it('appends the cache-busting version when one is given', async () => {
    const { imageLoader } = await loadLoader(storageEnv)

    expect(imageLoader({ src: MENTOR_UUID, version: 1754000000 })).toBe(
      `https://storage.example.com/mentor-images/${MENTOR_UUID}/full?v=1754000000`
    )
  })

  it('drops the bucket segment when a CDN endpoint fronts the bucket', async () => {
    const { imageLoader } = await loadLoader({
      ...storageEnv,
      NEXT_PUBLIC_CDN_ENDPOINT: 'images.openmentor.io',
    })

    expect(imageLoader({ src: MENTOR_UUID, quality: 'small' })).toBe(
      `https://images.openmentor.io/${MENTOR_UUID}/full`
    )
  })

  it('serves a localhost endpoint over http', async () => {
    const { imageLoader } = await loadLoader({
      NEXT_PUBLIC_S3_STORAGE_ENDPOINT: 'localhost:9000',
      NEXT_PUBLIC_S3_STORAGE_BUCKET: 'mentor-images',
    })

    expect(imageLoader({ src: MENTOR_UUID })).toBe(
      `http://localhost:9000/mentor-images/${MENTOR_UUID}/full`
    )
  })
})

describe('updatedAtToVersion', () => {
  it('turns a timestamp into whole seconds and rejects anything unusable', async () => {
    const { updatedAtToVersion } = await loadLoader({})

    expect(updatedAtToVersion('2026-08-01T10:00:00.500Z')).toBe(1785578400)
    expect(updatedAtToVersion(undefined)).toBeUndefined()
    expect(updatedAtToVersion('not a date')).toBeUndefined()
  })
})
