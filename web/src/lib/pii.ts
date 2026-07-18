/**
 * maskEmail redacts an email for logging: keeps the first local-part character
 * and the domain, masking the rest (e.g. "john@example.com" -> "j***@example.com").
 *
 * SECURITY/GDPR (L3): keeps raw addresses out of logs, which are shipped to
 * Grafana and would otherwise be a PII leak and an account-enumeration oracle
 * for anyone with log access. Non-address input is fully masked.
 */
export function maskEmail(email: unknown): string {
  if (typeof email !== 'string') return '***'
  const trimmed = email.trim()
  const at = trimmed.lastIndexOf('@')
  if (at <= 0) return '***'
  const local = trimmed.slice(0, at)
  const domain = trimmed.slice(at)
  if (local.length === 1) return `*${domain}`
  return `${local[0]}***${domain}`
}
