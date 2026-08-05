/**
 * The photo upload contract, in one place, because it is three numbers that
 * have to agree and drifted apart once already.
 *
 * 1. MAX_IMAGE_FILE_BYTES — what the picker validates and the copy promises.
 *    Must equal s3storage.MaxImageBytes in api/, which re-checks it server-side
 *    on the DECODED bytes.
 * 2. MAX_IMAGE_REQUEST_BODY — the Next bodyParser limit on the three proxy
 *    routes that forward the photo. The photo does NOT travel as a file: the
 *    forms read it with FileReader.readAsDataURL, so it arrives base64-encoded
 *    inside JSON at ~4/3 the raw size, plus the data: prefix and the rest of the
 *    registration form. A 10 MiB photo is ~13.6 MiB of request.
 * 3. middleware.MaxImageBodyBytes in api/ — the same number one hop further on.
 *
 * When (2) or (3) is sized as if it were (1), a photo at exactly the advertised
 * limit passes the picker and then 413s at a body reader, with a generic
 * too-large error instead of the friendly one the form knows how to show.
 *
 * config/__tests__/uploads.test.ts checks the encoded size against the limit and
 * checks that the proxy routes still declare it — Next requires the bodyParser
 * config to be a static literal, so those files cannot import this constant.
 */

/** Largest RAW image file the forms accept, before any encoding. */
export const MAX_IMAGE_FILE_BYTES = 10 * 1024 * 1024

/** How MAX_IMAGE_FILE_BYTES is written in user-facing copy. */
export const MAX_IMAGE_FILE_LABEL = '10 MB'

/**
 * The `sizeLimit` every route carrying a photo must declare, as the `bytes`
 * string Next parses. Kept in sync with middleware.MaxImageBodyBytes (14 MiB).
 */
export const MAX_IMAGE_REQUEST_BODY = '14mb'
