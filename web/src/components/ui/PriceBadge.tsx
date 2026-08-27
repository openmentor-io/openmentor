import { parsePrice } from '@/lib/price'
import type { JSX } from 'react'

/**
 * Price classification + badge (component sheet §pills/badges/price).
 *
 * `mentors.price` is a closed grammar since D87 — "Free", "Negotiable" or "$N"
 * — and lib/price is the only thing that reads it. This file used to carry a
 * byte-identical copy of config/filters' `parsePriceAmount`; both are gone.
 *
 * The three canonical looks:
 * - FREE       — mono 700, mint-ink on mint tint, radius 999
 * - $N         — mono 700, navy on surface, radius 999
 * - NEGOTIABLE — mono 500, ink-mute on surface, radius 999
 */
export default function PriceBadge({ price }: { price: string }): JSX.Element {
  const value = parsePrice(price)

  if (value?.kind === 'free') {
    return (
      <span className="inline-block rounded-full bg-brand-mint/[0.14] px-[11px] py-1.5 font-mono text-xs font-bold uppercase tracking-[0.04em] text-mint-ink">
        Free
      </span>
    )
  }

  if (value?.kind === 'negotiable') {
    return (
      <span className="inline-block rounded-full bg-surface px-[11px] py-1.5 font-mono text-xs font-medium uppercase tracking-[0.04em] text-ink-mute">
        Negotiable
      </span>
    )
  }

  // A fixed amount, or a stored value that predates the grammar — render it
  // verbatim rather than showing nothing.
  return (
    <span className="inline-block rounded-full bg-surface px-[11px] py-1.5 font-mono text-xs font-bold text-brand-navy">
      {price}
    </span>
  )
}
