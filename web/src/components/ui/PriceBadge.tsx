/**
 * Price classification + badge (component sheet §pills/badges/price).
 *
 * `mentors.price` is free text (DECISIONS D3): "Free", "$50", "$100 / hour",
 * "Negotiable"… The badge renders the three canonical looks:
 * - FREE       — mono 700, mint-ink on mint tint, radius 999
 * - $N / other — mono 700, navy on surface, radius 999
 * - NEGOTIABLE — mono 500, ink-mute on surface, radius 999
 */

export type PriceKind = 'free' | 'negotiable' | 'amount' | 'other'

/** Extract a numeric amount from a free-text price ("$100 / hour" -> 100). */
export function parsePriceAmount(price: string): number | null {
  const match = price.replace(/[,\s]/g, '').match(/(\d+(?:\.\d+)?)/)
  return match ? parseFloat(match[1]) : null
}

export function classifyPrice(price: string): { kind: PriceKind; amount: number | null } {
  const amount = parsePriceAmount(price)
  if (/free/i.test(price) || amount === 0) {
    return { kind: 'free', amount: 0 }
  }
  if (/negotiable/i.test(price)) {
    return { kind: 'negotiable', amount: null }
  }
  if (amount !== null) {
    return { kind: 'amount', amount }
  }
  return { kind: 'other', amount: null }
}

export default function PriceBadge({ price }: { price: string }): JSX.Element {
  const { kind } = classifyPrice(price)

  if (kind === 'free') {
    return (
      <span className="inline-block rounded-full bg-brand-mint/[0.14] px-[11px] py-1.5 font-mono text-xs font-bold uppercase tracking-[0.04em] text-mint-ink">
        Free
      </span>
    )
  }

  if (kind === 'negotiable') {
    return (
      <span className="inline-block rounded-full bg-surface px-[11px] py-1.5 font-mono text-xs font-medium uppercase tracking-[0.04em] text-ink-mute">
        Negotiable
      </span>
    )
  }

  return (
    <span className="inline-block rounded-full bg-surface px-[11px] py-1.5 font-mono text-xs font-bold text-brand-navy">
      {price}
    </span>
  )
}
