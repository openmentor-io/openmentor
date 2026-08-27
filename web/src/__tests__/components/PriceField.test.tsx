import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import PriceField from '@/components/forms/PriceField'
import type { PriceValue } from '@/lib/price'

/**
 * The control's own contract. The three forms that embed it are covered where
 * they live (ProfileForm, RegisterMentorForm, the admin editor); this pins the
 * behaviour none of them can see from the outside — that the `$` is a prefix
 * ELEMENT and never enters the value, and that the third pill announces itself
 * as an affordance before it announces an amount.
 */
function Harness({ initial = null }: { initial?: PriceValue | null }): JSX.Element {
  const [value, setValue] = useState<PriceValue | null>(initial)
  return (
    <>
      <PriceField value={value} onChange={setValue} label="Price per one-hour session" />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </>
  )
}

const amountBox = (): HTMLInputElement =>
  screen.getByLabelText(/Price in US dollars/i) as HTMLInputElement
const pill = (name: RegExp): HTMLInputElement =>
  screen.getByRole('radio', { name }) as HTMLInputElement
const value = (): PriceValue | null => JSON.parse(screen.getByTestId('value').textContent || 'null')

describe('PriceField', () => {
  it('starts with nothing selected and no amount box', () => {
    render(<Harness />)

    expect(pill(/^Free$/i)).not.toBeChecked()
    expect(pill(/^Negotiable$/i)).not.toBeChecked()
    expect(pill(/Set a fixed amount/i)).not.toBeChecked()
    expect(screen.queryByLabelText(/Price in US dollars/i)).not.toBeInTheDocument()
  })

  // The mockup draws the third pill as the VALUE ("$50"), which says nothing
  // about the amount being the mentor's to set — and reads as an empty pill
  // during registration. It states its action until there is an amount.
  it('labels the third pill as an action until an amount exists', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    expect(screen.getByText('Set an amount')).toBeInTheDocument()

    await user.click(pill(/Set a fixed amount/i))
    await user.type(amountBox(), '120')

    expect(screen.getByText('$120')).toBeInTheDocument()
    expect(screen.queryByText('Set an amount')).not.toBeInTheDocument()
    // The accessible name keeps a stable stem rather than becoming a bare
    // number once the visible label changes.
    expect(pill(/Fixed amount, \$120/i)).toBeChecked()
  })

  it('keeps the $ out of the value', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    await user.type(amountBox(), '50')

    // The sigil renders, but as its own element — putting it in the value
    // produces "$$50" and jumps the caret on every keystroke.
    expect(screen.getByText('$', { selector: 'span' })).toBeInTheDocument()
    expect(amountBox().value).toBe('50')
    expect(value()).toEqual({ kind: 'fixed', amount: 50 })
  })

  // The two rounds of the same PR finding, resolved by one rule: what the
  // mentor typed is what the box SHOWS. Clean amounts land as amounts;
  // anything else lands verbatim as an invalid draft with amount null — no
  // keystroke is swallowed for later digits to concatenate around ("50.5"
  // must not end as a valid $505), and nothing invalid can save.
  it.each([
    ['0', '', null, 'a lone zero clears'],
    ['007', '7', 7, 'leading zeros are decoration'],
    ['1000', '1000', 1000, 'MAX_AMOUNT stays typable'],
    ['12345', '12345', 12345, 'over-the-cap lands and reads as invalid, never truncates to $1234'],
    ['50.5', '50.5', null, 'the dot lands visibly; amount is null, save blocked'],
    ['5a0', '5a0', null, 'a stray letter lands visibly instead of merging 5 and 0 into 50'],
    ['abc', 'abc', null, 'letters land visibly and read as invalid'],
    ['-50', '-50', null, 'a sign lands visibly instead of becoming 50'],
  ] as [string, string, number | null, string][])(
    'typed %p shows %p with amount %p (%s)',
    async (typed, shown, amount) => {
      const user = userEvent.setup()
      render(<Harness />)

      await user.click(pill(/Set a fixed amount/i))
      await user.type(amountBox(), typed)

      expect(amountBox().value).toBe(shown)
      expect(value()).toEqual({ kind: 'fixed', amount })
    }
  )

  it('recovers from an invalid draft the moment the input is clean again', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    await user.type(amountBox(), '50.5')
    expect(value()).toEqual({ kind: 'fixed', amount: null })

    await user.clear(amountBox())
    await user.type(amountBox(), '505')

    expect(amountBox().value).toBe('505')
    expect(value()).toEqual({ kind: 'fixed', amount: 505 })
  })

  // A paste of a value that is not a plain amount lands verbatim as an
  // invalid draft — its digits are never merged into a different number
  // ("50.5" must not become $505, "-50" must not become $50).
  it.each([
    ['50.5'],
    ['-50'],
    ['1,0,00'],
  ])('a pasted %p lands verbatim with amount null', async (pasted) => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    fireEvent.change(amountBox(), { target: { value: pasted } })

    expect(amountBox().value).toBe(pasted)
    expect(value()).toEqual({ kind: 'fixed', amount: null })
  })

  it('accepts a pasted well-formed "1,000" as the same number', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    fireEvent.change(amountBox(), { target: { value: '1,000' } })

    expect(amountBox().value).toBe('1000')
    expect(value()).toEqual({ kind: 'fixed', amount: 1000 })
  })

  // Without this, the form submits {kind: 'free', amount: 120}, which
  // mentors_price_chk rejects as a 500 from the repository rather than as a
  // field error the mentor can act on.
  // An external value replacement (an admin restore loading the server's
  // price, a form reset) must win the display over a lingering invalid draft:
  // otherwise the box shows one thing while Save submits another.
  it('shows an externally set amount over a stale invalid draft', async () => {
    const user = userEvent.setup()

    function ReplacingHarness(): JSX.Element {
      const [value, setValue] = useState<PriceValue | null>({ kind: 'fixed', amount: null })
      return (
        <>
          <PriceField value={value} onChange={setValue} label="Price per one-hour session" />
          <button type="button" onClick={() => setValue({ kind: 'fixed', amount: 137 })}>
            restore
          </button>
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      )
    }
    render(<ReplacingHarness />)

    fireEvent.change(amountBox(), { target: { value: '50.5' } })
    expect(amountBox().value).toBe('50.5')

    await user.click(screen.getByRole('button', { name: 'restore' }))

    expect(amountBox().value).toBe('137')
    expect(value()).toEqual({ kind: 'fixed', amount: 137 })
    expect(amountBox().className).not.toContain('field-error')
  })

  it('clears an invalid draft when the mentor switches pills', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    fireEvent.change(amountBox(), { target: { value: '50.5' } })

    await user.click(pill(/^Free$/i))
    await user.click(pill(/Set a fixed amount/i))

    expect(amountBox().value).toBe('')
    expect(value()).toEqual({ kind: 'fixed', amount: null })
  })

  it('drops the amount when the mentor switches away from a fixed price', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(pill(/Set a fixed amount/i))
    await user.type(amountBox(), '120')
    expect(value()).toEqual({ kind: 'fixed', amount: 120 })

    await user.click(pill(/^Free$/i))

    expect(value()).toEqual({ kind: 'free', amount: null })
    expect(screen.queryByLabelText(/Price in US dollars/i)).not.toBeInTheDocument()
  })

  it('prefills from a stored fixed price without stealing focus', () => {
    render(<Harness initial={{ kind: 'fixed', amount: 75 }} />)

    expect(pill(/Fixed amount, \$75/i)).toBeChecked()
    expect(amountBox().value).toBe('75')
    // Opening a profile that already has a price must not yank focus into the
    // amount box — only choosing the pill does that.
    expect(amountBox()).not.toHaveFocus()
  })

  // Caught in a browser, not by a test: Tailwind orders its own variants, so a
  // bare `peer-hover:bg-surface` beats `peer-checked:bg-brand-navy` whatever
  // order they appear in the class string — the selected pill kept its white
  // text on the light hover fill for as long as the pointer rested on it, which
  // is most of the time right after clicking it. jsdom computes no Tailwind, so
  // this asserts the shape of the rule instead of the rendered colour.
  it('scopes the pill hover fill to unchecked pills', () => {
    render(<Harness initial={{ kind: 'free', amount: null }} />)

    const label = screen.getByText('Free')
    const classes = label.className

    expect(classes).toContain('peer-checked:bg-brand-navy')
    expect(classes).toContain('peer-[:hover:not(:checked)]:bg-surface')
    expect(classes).not.toMatch(/(^|\s)peer-hover:bg-/)
  })

  it('exposes the pills as one named radio group', () => {
    render(<Harness />)

    expect(
      screen.getByRole('group', { name: /Price per one-hour session/i })
    ).toBeInTheDocument()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
  })
})
