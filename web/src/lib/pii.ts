/**
 * maskEmail redacts an email for logging: keeps the first local-part character
 * and the whole domain, masking the rest (e.g. "john@example.com" ->
 * "j***@example.com").
 *
 * SECURITY/GDPR (L3, C11): keeps raw addresses out of logs, which are shipped to
 * Grafana and would otherwise be a PII leak and an account-enumeration oracle for
 * anyone with log access. Non-address input is fully masked.
 *
 * THIS IS ONE OF EXACTLY TWO IMPLEMENTATIONS, and the other one is authoritative.
 * C11 found three copies of this function, which is why it drifted: the second Go
 * copy is gone and the rule now lives in api/pkg/redact.Email. Go and TypeScript
 * cannot share the code, so the two survivors are held together by a shared
 * fixture instead — api/pkg/redact/testdata/email_masking.json, which this file's
 * test and the Go package's test both read. Change the rule there first; a change
 * made only on one side fails on that side.
 */
export function maskEmail(email: unknown): string {
  if (typeof email !== 'string') return '***'
  const trimmed = email.trim()
  // Empty stays empty, so an absent field does not become a masked one.
  if (trimmed === '') return ''

  // lastIndexOf, not indexOf: a local part may legally contain a quoted '@' and a
  // domain never does.
  const at = trimmed.lastIndexOf('@')
  // No local part or no domain means this is not an address, and the input could
  // be anything — including a token — so nothing in it is safe to keep.
  if (at <= 0 || at === trimmed.length - 1) return '***'

  const local = trimmed.slice(0, at)
  const domain = trimmed.slice(at)
  if (local.length === 1) return `*${domain}`
  return `${local[0]}***${domain}`
}
