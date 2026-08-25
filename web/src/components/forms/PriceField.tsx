import { useId, useRef, useState, type ChangeEvent, type JSX, type Ref } from 'react'
import classNames from 'classnames'
import {
  MAX_AMOUNT,
  MIN_AMOUNT,
  sanitizeAmountInput,
  isValidAmount,
  type PriceKind,
  type PriceValue,
} from '@/lib/price'

/**
 * The one price control, shared by the profile edit form, the registration form
 * and the admin editor (D87). Written once because the sanitising, the clearing
 * on kind change and the `$` prefix are the kind of logic that drifts when it is
 * written three times — which is exactly how two byte-identical
 * `parsePriceAmount` implementations ended up in the codebase.
 *
 * SHAPE — three pills, per `09 Profile Edit.dc.html`, with three deliberate
 * deviations from that mockup:
 *
 *  1. The mockup's middle pill is drawn as the VALUE ("$50"), which says nothing
 *     about the amount being the mentor's to set — and in registration, where
 *     there is no stored amount yet, it degrades to an empty pill. Here the
 *     third pill reads "Set an amount" until there is one, then shows it.
 *  2. It is LAST rather than middle: its width changes as digits are typed, and
 *     in the middle that reflows the pills beside it on every keystroke.
 *  3. Editing happens in a `.field` below rather than inside the navy pill, so
 *     the focus ring, the error state and the hint come from the existing
 *     primitive instead of being reinvented on a navy background.
 *
 * It is a controlled component over {kind, amount} rather than over the stored
 * string: react-hook-form wraps it in a <Controller>, the admin editor drives it
 * from its own state, and the {kind, amount} pair is what a price_kind /
 * price_amount column pair would hold if the storage is ever typed.
 */

interface PriceFieldProps {
  /**
   * Null is "nothing chosen yet" — registration starts there, and so does a
   * profile whose stored value does not parse. Nothing is preselected in either
   * case ON PURPOSE: an uncontrolled <select> silently reporting its first
   * option is what rewrote stored prices to "Free" on unrelated saves (D44), and
   * defaulting this control would reintroduce the same silent write.
   */
  value: PriceValue | null
  onChange: (next: PriceValue) => void
  /** Visible group label; rendered as the fieldset's legend. */
  label: string
  labelClassName?: string
  /** Appended to the legend — the callers' shared required marker. */
  requiredMark?: JSX.Element
  /** Marks the control as failing validation and renders `error` beneath it. */
  invalid?: boolean
  error?: string
  /** Distinguishes the radio group when two forms share a page. */
  name?: string
  /**
   * Attached to the first pill. react-hook-form focuses the first invalid field
   * on a blocked submit, which is what scrolls the error into view instead of
   * leaving Save looking inert — and a <Controller> can only do that if it gets
   * a ref to something focusable.
   */
  inputRef?: Ref<HTMLInputElement>
}

const PILL_BASE =
  'block cursor-pointer select-none rounded-full px-[14px] py-2 text-center font-sans text-xs font-semibold ' +
  'transition-colors duration-150 border-[1.5px] border-line bg-white text-brand-navy ' +
  // Scoped to :not(:checked) deliberately. Tailwind orders its own variants, so
  // a plain peer-hover:bg-surface wins over peer-checked:bg-brand-navy whatever
  // order they are written in — leaving the selected pill's white text on the
  // light hover fill for as long as the pointer rests on it.
  'peer-[:hover:not(:checked)]:bg-surface ' +
  'peer-checked:border-brand-navy peer-checked:bg-brand-navy peer-checked:text-white ' +
  'peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 ' +
  'peer-focus-visible:outline-brand-cobalt'

export default function PriceField({
  value,
  onChange,
  label,
  labelClassName,
  requiredMark,
  invalid = false,
  error,
  name = 'price-kind',
  inputRef,
}: PriceFieldProps): JSX.Element {
  const groupId = useId()
  const amountId = `${groupId}-amount`
  const hintId = `${groupId}-hint`
  const amountRef = useRef<HTMLInputElement>(null)

  const amount = value?.amount ?? null
  const isFixed = value?.kind === 'fixed'

  // What the box shows when the last edit was NOT a clean amount ("50.5",
  // "-50", "5a0"). PR review, twice over: the first pass rejected such edits
  // outright, which for TYPED input let later digits concatenate around the
  // refused character ("50.5" keystrokes ended as a valid $505 — a price the
  // mentor never typed). So invalid input now LANDS, verbatim and visibly,
  // with amount null — the save is blocked and the range/whole-amount error
  // says why. Nothing is reconstructed, nothing disappears, nothing saves.
  const [draft, setDraft] = useState<string | null>(null)
  const amountText = draft ?? (amount === null ? '' : String(amount))

  const handleAmountChange = (raw: string): void => {
    const digits = sanitizeAmountInput(raw)
    if (digits === null) {
      // Bounded only to protect the layout: this text is already invalid, so
      // capping it rewrites nothing a mentor could save.
      setDraft(raw.slice(0, 20))
      onChange({ kind: 'fixed', amount: null })
      return
    }
    setDraft(null)
    onChange({ kind: 'fixed', amount: digits === '' ? null : Number(digits) })
  }

  const selectKind = (kind: PriceKind): void => {
    setDraft(null)
    // Switching away from `fixed` drops the amount rather than parking it: a
    // form that kept it would submit {kind: 'free', amount: 120}, which the
    // paired database constraint rejects as a 500 from the repository instead
    // of as a field error the mentor can act on.
    onChange({ kind, amount: kind === 'fixed' ? amount : null })
    if (kind === 'fixed') {
      // Focus here rather than in an effect so opening a profile that already
      // has a fixed price does not steal focus on mount.
      window.requestAnimationFrame(() => amountRef.current?.focus())
    }
  }

  const pills: { kind: PriceKind; label: string; ariaLabel: string; wide: boolean }[] = [
    { kind: 'free', label: 'Free', ariaLabel: 'Free', wide: false },
    { kind: 'negotiable', label: 'Negotiable', ariaLabel: 'Negotiable', wide: false },
    {
      kind: 'fixed',
      label: isValidAmount(amount) ? `$${amount}` : 'Set an amount',
      // The visible label changes once an amount exists, so the accessible name
      // keeps a stable stem instead of turning into a bare number.
      ariaLabel: isValidAmount(amount) ? `Fixed amount, $${amount}` : 'Set a fixed amount',
      wide: true,
    },
  ]

  return (
    <fieldset className="min-w-0">
      <legend className={labelClassName}>
        {label}
        {requiredMark}
      </legend>

      <div className="flex flex-wrap gap-2">
        {pills.map((pill, index) => (
          <label
            key={pill.kind}
            className={classNames(
              'min-w-0',
              pill.wide ? 'basis-full sm:basis-auto' : 'flex-1 basis-0 sm:flex-none sm:basis-auto'
            )}
          >
            <input
              ref={index === 0 ? inputRef : undefined}
              type="radio"
              className="peer sr-only"
              name={name}
              value={pill.kind}
              checked={value?.kind === pill.kind}
              aria-label={pill.ariaLabel}
              onChange={() => selectKind(pill.kind)}
            />
            <span className={classNames(PILL_BASE, invalid && 'border-danger')}>{pill.label}</span>
          </label>
        ))}
      </div>

      {isFixed && (
        <div className="mt-3">
          <div className="relative">
            {/* A prefix element, never part of the value: putting the sigil in
                the value produces "$$50", jumps the caret on every keystroke and
                hands the form a string that is not a number. */}
            <span
              aria-hidden="true"
              className="pointer-events-none absolute left-[15px] top-1/2 -translate-y-1/2 font-sans text-sm text-ink-soft"
            >
              $
            </span>
            <input
              ref={amountRef}
              id={amountId}
              type="text"
              inputMode="numeric"
              autoComplete="off"
              value={amountText}
              aria-label="Price in US dollars per one-hour session"
              aria-describedby={hintId}
              onChange={(event: ChangeEvent<HTMLInputElement>) => handleAmountChange(event.target.value)}
              className={classNames('field pl-7', (invalid || draft !== null) && 'field-error')}
            />
          </div>
          <p id={hintId} className="mt-1.5 text-sm text-ink-soft">
            Any whole amount from ${MIN_AMOUNT} to ${MAX_AMOUNT}.
          </p>
        </div>
      )}

      {error && (
        <p className="mt-2 text-sm font-medium text-danger" role="alert">
          {error}
        </p>
      )}
    </fieldset>
  )
}
