import sanitizeHtml from 'sanitize-html'

/**
 * SECURITY: Sanitize user-generated HTML content to prevent XSS attacks
 * This function helps to migrate HTML from one wysiwyg to another
 * and ensures all content is properly sanitized
 *
 * @param html - Raw HTML content from user
 * @returns Sanitized HTML safe for rendering
 */
export function htmlContent(html: string | null | undefined): string {
  if (!html) return ''

  // First, handle wysiwyg v1 (airtable) format migration
  let processedHTML = html
  if (!html.startsWith('<p>')) {
    // A run of newlines becomes a <br/> when a list marker follows it, and a
    // paragraph break otherwise. The marker is matched as an *optional* suffix
    // so a run is never re-scanned: the previous `/\n+([0-9-])/g` backtracked
    // through every newline of a run that turned out not to be followed by a
    // marker, making this quadratic in the length of the run (CWE-1333) on
    // profile text the mentor controls.
    processedHTML =
      '<p>' +
      html.replace(/\n+([0-9-])?/g, (_match: string, marker?: string) =>
        marker ? `<br/>${marker}` : '</p><p>'
      ) +
      '</p>'
  }

  // SECURITY: Sanitize HTML to prevent XSS attacks
  // Only allow safe tags and attributes
  const clean = sanitizeHtml(processedHTML, {
    allowedTags: [
      'p',
      'br',
      'strong',
      'b',
      'em',
      'i',
      'u',
      's',
      'a',
      'ul',
      'ol',
      'li',
      'h2',
      'h3',
      'h4',
      'blockquote',
      'code',
      'pre',
    ],
    allowedAttributes: {
      a: ['href', 'target', 'rel'],
    },
    // Disallow all URL schemes except http(s) and mailto
    allowedSchemes: ['http', 'https', 'mailto'],
    // Transform all links to be safe
    transformTags: {
      a: (_tagName, attribs) => {
        return {
          tagName: 'a',
          attribs: {
            ...attribs,
            target: '_blank',
            rel: 'noopener noreferrer',
          },
        }
      },
    },
  })

  return clean
}
