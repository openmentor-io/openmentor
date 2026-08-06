import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import DeleteProfileDialog, { confirmationMatches } from '@/components/ui/DeleteProfileDialog'

const USERNAME = 'anna-petrova-42'

function renderDialog(overrides: Partial<React.ComponentProps<typeof DeleteProfileDialog>> = {}) {
  const props = {
    isOpen: true,
    onClose: jest.fn(),
    onConfirm: jest.fn().mockResolvedValue(undefined),
    username: USERNAME,
    title: 'Delete your profile',
    children: <p>Everything goes.</p>,
    ...overrides,
  }
  render(<DeleteProfileDialog {...props} />)
  return props
}

function confirmButton(): HTMLElement {
  return screen.getByRole('button', { name: /delete profile permanently/i })
}

describe('DeleteProfileDialog', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('renders nothing when closed', () => {
    renderDialog({ isOpen: false })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows the irreversibility warning and the username to type', () => {
    renderDialog()

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('This cannot be undone')).toBeInTheDocument()
    expect(screen.getByText(USERNAME)).toBeInTheDocument()
  })

  // The gate is the whole point of the dialog.
  it('keeps the delete button disabled until the username is typed', () => {
    renderDialog()
    const input = screen.getByLabelText(/to confirm/i)

    expect(confirmButton()).toBeDisabled()

    fireEvent.change(input, { target: { value: 'anna' } })
    expect(confirmButton()).toBeDisabled()

    fireEvent.change(input, { target: { value: 'some-other-mentor' } })
    expect(confirmButton()).toBeDisabled()

    fireEvent.change(input, { target: { value: USERNAME } })
    expect(confirmButton()).toBeEnabled()
  })

  it('accepts the username in any case, with surrounding whitespace', () => {
    renderDialog()
    const input = screen.getByLabelText(/to confirm/i)

    fireEvent.change(input, { target: { value: '  ANNA-Petrova-42  ' } })
    expect(confirmButton()).toBeEnabled()
  })

  it('confirms only once the username matches, passing the typed value through', async () => {
    const props = renderDialog()

    // A case variant the gate accepts but that differs from the canonical
    // username — the callback must receive it verbatim, because callers send
    // it to the server as the confirmation.
    fireEvent.change(screen.getByLabelText(/to confirm/i), {
      target: { value: 'ANNA-Petrova-42' },
    })
    fireEvent.click(confirmButton())

    await waitFor(() => expect(props.onConfirm).toHaveBeenCalledTimes(1))
    expect(props.onConfirm).toHaveBeenCalledWith('ANNA-Petrova-42')
  })

  it('surfaces the failure inside the dialog and stays open', async () => {
    const props = renderDialog({
      onConfirm: jest.fn().mockRejectedValue(new Error('The username you entered does not match')),
    })

    fireEvent.change(screen.getByLabelText(/to confirm/i), { target: { value: USERNAME } })
    fireEvent.click(confirmButton())

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('The username you entered does not match')
    )
    // Still open, so the admin can correct the typing rather than start over.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(props.onClose).not.toHaveBeenCalled()
  })

  it('closes on cancel without confirming', () => {
    const props = renderDialog()

    fireEvent.change(screen.getByLabelText(/to confirm/i), { target: { value: USERNAME } })
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

    expect(props.onClose).toHaveBeenCalled()
    expect(props.onConfirm).not.toHaveBeenCalled()
  })

  // Mirrors the server rule (services.usernameConfirmationMatches). A client
  // that disagrees either disables a valid delete or sends one that 400s.
  describe('confirmationMatches', () => {
    it.each([
      ['anna-petrova-42', true],
      ['ANNA-PETROVA-42', true],
      ['  anna-petrova-42  ', true],
      ['anna-petrova', false],
      ['', false],
      ['anna petrova 42', false],
    ])('%s -> %s', (typed, expected) => {
      expect(confirmationMatches(typed, USERNAME)).toBe(expected)
    })
  })
})
