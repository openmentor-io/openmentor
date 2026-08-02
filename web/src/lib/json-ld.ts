/**
 * Props for a JSON-LD `<script>`, spread onto a NATIVE `<script>` element:
 *
 *   <script {...jsonLdScriptProps(data)} />
 *
 * It must stay a native element rather than a `<JsonLd>` wrapper component.
 * next/head buckets its children by element type and only reconciles
 * meta/base/link/style/script, so a custom component is invisible to it: the
 * schema never appears on client-side navigation, and one rendered on a hard
 * load is never cleaned up when navigating away (leaving, say, the FAQ schema
 * on /privacy, or one mentor's Person data on another mentor's page).
 *
 * Security: payloads embed user-controlled strings (mentor names, job titles,
 * bios). `<`, `>` and `&` become \uXXXX escapes — valid JSON that parses back
 * to the original characters, but inert as raw source text, so a payload can
 * never close the <script> element or open a new one. Do not remove this.
 */
export function jsonLdScriptProps(data: Record<string, unknown>): {
  type: string
  dangerouslySetInnerHTML: { __html: string }
} {
  const json = JSON.stringify(data)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')

  return {
    type: 'application/ld+json',
    dangerouslySetInnerHTML: { __html: json },
  }
}
