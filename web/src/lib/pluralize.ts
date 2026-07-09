/**
 * Simple English pluralization helper.
 * @param count - Quantity for the word
 * @param singular - Singular form. Example: 'mentee'
 * @param plural - Plural form. Defaults to singular + 's'
 * @returns The form matching the count
 */
export default function pluralize(count: number, singular: string, plural?: string): string {
  return count === 1 ? singular : plural ?? `${singular}s`
}
