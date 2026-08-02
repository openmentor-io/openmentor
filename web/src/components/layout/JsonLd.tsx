interface JsonLdProps {
  data: Record<string, unknown>
}

/**
 * Renders a `<script type="application/ld+json">` block.
 *
 * Security: the payload can embed user-controlled strings (mentor names, job
 * titles, bios). `dangerouslySetInnerHTML` is required here — React's normal
 * text escaping would corrupt the JSON — so we escape `<`, `>`, and `&` as
 * `\uXXXX` sequences ourselves. Those decode back to the original characters
 * when a consumer JSON.parses the script contents, but as raw source text
 * they can no longer close the `<script>` tag or open a new one. Do not
 * remove this escaping.
 */
export default function JsonLd({ data }: JsonLdProps): JSX.Element {
  const json = JSON.stringify(data)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')

  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: json }} />
}
