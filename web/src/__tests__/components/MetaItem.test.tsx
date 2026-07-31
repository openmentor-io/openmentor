import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import MetaItem from '@/components/mentor-admin/MetaItem'

const LONG_CONTACT = 'email or telegram: @daria_andronikova_mentorship_account'

describe('MetaItem', () => {
  it('renders the full value instead of clipping it', () => {
    render(<MetaItem label="Contact" value={LONG_CONTACT} copyable />)
    // The old layout truncated with an ellipsis, hiding the handle the
    // mentor actually needs — the whole string must stay in the DOM.
    expect(screen.getByText(LONG_CONTACT)).toBeInTheDocument()
  })

  // Keeping the row one line tall is what stops a 100-character contact line
  // from pushing the rest of the card around; scrolling replaces truncation.
  it('keeps the value on one scrollable line', () => {
    const { container } = render(<MetaItem label="Contact" value={LONG_CONTACT} />)
    const line = container.querySelector('.scroll-line')
    expect(line).toHaveTextContent(LONG_CONTACT)
    expect(line).toHaveAttribute('title', LONG_CONTACT)
    expect(line?.className).not.toContain('truncate')
  })

  it('renders an href value as a link', () => {
    render(
      <MetaItem
        label="Email"
        value="andronikova.daria@example.com"
        href="mailto:andronikova.daria@example.com"
      />
    )
    expect(screen.getByRole('link', { name: 'andronikova.daria@example.com' })).toHaveAttribute(
      'href',
      'mailto:andronikova.daria@example.com'
    )
  })

  it('copies the value to the clipboard', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })

    render(<MetaItem label="Contact" value={LONG_CONTACT} copyable />)
    await userEvent.click(screen.getByRole('button', { name: 'Copy contact' }))

    expect(writeText).toHaveBeenCalledWith(LONG_CONTACT)
    expect(await screen.findByRole('button', { name: 'Contact copied' })).toBeInTheDocument()
  })

  it('falls back to a prompt when the clipboard is unavailable', async () => {
    Object.assign(navigator, {
      clipboard: { writeText: jest.fn().mockRejectedValue(new Error('denied')) },
    })
    const prompt = jest.spyOn(window, 'prompt').mockReturnValue(null)

    render(<MetaItem label="Contact" value={LONG_CONTACT} copyable />)
    await userEvent.click(screen.getByRole('button', { name: 'Copy contact' }))

    expect(prompt).toHaveBeenCalledWith('Copy contact:', LONG_CONTACT)
    prompt.mockRestore()
  })

  it('omits the copy button for values that are not copyable', () => {
    render(<MetaItem label="Level" value="Middle" />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
