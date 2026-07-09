import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ContactMentorForm from '@/components/forms/ContactMentorForm'

// Mock Turnstile component
jest.mock('@marsidev/react-turnstile', () => ({
  __esModule: true,
  Turnstile: function MockTurnstile({ onSuccess }: { onSuccess?: (token: string) => void }) {
    return (
      <button type="button" data-testid="turnstile" onClick={() => onSuccess?.('mock-turnstile-token')}>
        Complete Turnstile
      </button>
    )
  },
}))

describe('ContactMentorForm', () => {
  const mockOnSubmit = jest.fn()

  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('renders all required form fields', () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByLabelText(/Your email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Your full name/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/What would you like to talk about/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Telegram \(optional\)/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/How would you rate your level/i)).toBeInTheDocument()
  })

  it('renders submit button', () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByRole('button', { name: /Send request/i })).toBeInTheDocument()
  })

  it('disables submit button when isLoading is true', () => {
    render(<ContactMentorForm isLoading={true} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByRole('button', { name: /Send request/i })).toBeDisabled()
  })

  it('displays error message when isError is true', () => {
    render(<ContactMentorForm isLoading={false} isError={true} onSubmit={mockOnSubmit} />)

    expect(screen.getByText(/Something went wrong/i)).toBeInTheDocument()
  })

  it('shows validation error for empty required fields on submit', async () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const submitButton = screen.getByRole('button', { name: /Send request/i })

    await act(async () => {
      fireEvent.click(submitButton)
    })

    await waitFor(() => {
      // Should show required field errors
      const requiredErrors = screen.getAllByText(/This field is required/i)
      expect(requiredErrors.length).toBeGreaterThan(0)
    })

    // Form should not be submitted
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  it('renders experience level dropdown with options', () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const select = screen.getByLabelText(/How would you rate your level/i)
    expect(select).toBeInTheDocument()

    // Check some options exist
    expect(screen.getByRole('option', { name: 'Junior' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Middle' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Senior' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'C-level' })).toBeInTheDocument()
  })

  it('renders the turnstile captcha component', () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByTestId('turnstile')).toBeInTheDocument()
  })

  it('allows selecting experience level', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const select = screen.getByLabelText(/How would you rate your level/i)
    await user.selectOptions(select, 'Senior')

    expect(select).toHaveValue('Senior')
  })

  it('submits form with valid data', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Fill all required fields
    await user.type(screen.getByLabelText(/Your email/i), 'test@example.com')
    await user.type(screen.getByLabelText(/Your full name/i), 'John Doe')
    await user.type(
      screen.getByLabelText(/What would you like to talk about/i),
      'I need help with my career development in tech industry.'
    )
    await user.type(screen.getByLabelText(/Telegram \(optional\)/i), '@johndoe')

    // Complete the captcha
    await act(async () => {
      fireEvent.click(screen.getByTestId('turnstile'))
    })

    // Submit form
    const submitButton = screen.getByRole('button', { name: /Send request/i })

    await act(async () => {
      fireEvent.click(submitButton)
    })

    await waitFor(() => {
      expect(mockOnSubmit).toHaveBeenCalledTimes(1)
    })

    expect(mockOnSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        email: 'test@example.com',
        name: 'John Doe',
        intro: 'I need help with my career development in tech industry.',
        telegramUsername: '@johndoe',
        captchaToken: 'mock-turnstile-token',
      }),
      expect.anything() // react-hook-form passes event as second arg
    )
  })

  it('submits form without a telegram username (optional field)', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Fill all required fields, leave telegram empty
    await user.type(screen.getByLabelText(/Your email/i), 'test@example.com')
    await user.type(screen.getByLabelText(/Your full name/i), 'John Doe')
    await user.type(
      screen.getByLabelText(/What would you like to talk about/i),
      'I need help with my career development in tech industry.'
    )

    // Complete the captcha
    await act(async () => {
      fireEvent.click(screen.getByTestId('turnstile'))
    })

    // Submit form
    const submitButton = screen.getByRole('button', { name: /Send request/i })

    await act(async () => {
      fireEvent.click(submitButton)
    })

    await waitFor(() => {
      expect(mockOnSubmit).toHaveBeenCalledTimes(1)
    })

    expect(mockOnSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        email: 'test@example.com',
        telegramUsername: '',
      }),
      expect.anything()
    )
  })

  it('does not submit without a captcha token', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Fill all required fields except the captcha
    await user.type(screen.getByLabelText(/Your email/i), 'test@example.com')
    await user.type(screen.getByLabelText(/Your full name/i), 'John Doe')
    await user.type(
      screen.getByLabelText(/What would you like to talk about/i),
      'I need help with my career development in tech industry.'
    )
    await user.type(screen.getByLabelText(/Telegram \(optional\)/i), '@johndoe')

    // Don't complete the captcha - submit form
    const submitButton = screen.getByRole('button', { name: /Send request/i })

    await act(async () => {
      fireEvent.click(submitButton)
    })

    // Wait for validation to complete
    await waitFor(() => {
      // Form should not be submitted without a captcha token
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })
  })
})
