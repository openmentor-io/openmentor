import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ContactMentorForm from '@/components/forms/ContactMentorForm'

// Mock Turnstile component (forwardRef so the widget's reset() is callable)
const mockTurnstileReset = jest.fn()
let freshTokenSeq = 0
jest.mock('@marsidev/react-turnstile', () => {
  const react = jest.requireActual('react')
  return {
    __esModule: true,
    Turnstile: react.forwardRef(function MockTurnstile(
      { onSuccess }: { onSuccess?: (token: string) => void },
      ref: React.Ref<{ reset: () => void }>
    ) {
      react.useImperativeHandle(ref, () => ({
        reset: () => {
          mockTurnstileReset()
          // The real widget re-solves the challenge asynchronously. Issuing the
          // token synchronously here would have it wiped by the setValue('')
          // that follows reset() in the component's effect.
          freshTokenSeq += 1
          const token = `fresh-turnstile-token-${freshTokenSeq}`
          setTimeout(() => onSuccess?.(token), 0)
        },
      }))
      return (
        <button
          type="button"
          data-testid="turnstile"
          onClick={() => onSuccess?.('mock-turnstile-token')}
        >
          Complete Turnstile
        </button>
      )
    }),
  }
})

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
    expect(screen.getByLabelText(/How can your mentor reach you/i)).toBeInTheDocument()
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
    await user.type(screen.getByLabelText(/How can your mentor reach you/i), '@johndoe')

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
        contact: '@johndoe',
        captchaToken: 'mock-turnstile-token',
      }),
      expect.anything() // react-hook-form passes event as second arg
    )
  })

  it('submits form without contact details (optional field)', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Fill all required fields, leave the contact field empty
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
        contact: '',
      }),
      expect.anything()
    )
  })

  it('shows a pattern error for an invalid email address', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    await user.type(screen.getByLabelText(/Your email/i), 'not-an-email')

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send request/i }))
    })

    await waitFor(() => {
      expect(screen.getByText(/That doesn't look like a full email address/i)).toBeInTheDocument()
    })

    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  it('applies the error field style to invalid fields on submit', async () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send request/i }))
    })

    await waitFor(() => {
      expect(screen.getByLabelText(/Your email/i)).toHaveClass('field-error')
    })
    expect(screen.getByLabelText(/Your full name/i)).toHaveClass('field-error')
    expect(screen.getByLabelText(/What would you like to talk about/i)).toHaveClass('field-error')
    // Valid (optional) field keeps the default style
    expect(screen.getByLabelText(/How can your mentor reach you/i)).not.toHaveClass('field-error')
  })

  it('shows a live character counter for the intro field', async () => {
    const user = userEvent.setup()
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByText('0/4000')).toBeInTheDocument()

    await user.type(screen.getByLabelText(/What would you like to talk about/i), 'Hello')

    expect(screen.getByText('5/4000')).toBeInTheDocument()
  })

  it('mentions the mentor first name in the consent fine print', () => {
    render(
      <ContactMentorForm
        isLoading={false}
        isError={false}
        onSubmit={mockOnSubmit}
        mentorFirstName="Jonas"
      />
    )

    expect(screen.getByText(/We only share your email with\s+Jonas/i)).toBeInTheDocument()
  })

  it('falls back to a generic consent fine print without a mentor name', () => {
    render(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByText(/We only share your email with\s+the mentor/i)).toBeInTheDocument()
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
    await user.type(screen.getByLabelText(/How can your mentor reach you/i), '@johndoe')

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

  // Turnstile tokens are single-use: siteverify has consumed this one by the
  // time the submit fails, so a retry carrying the same token can only fail
  // again — while the form stays mounted with a live submit button.
  it('resets the captcha after a failed submit so the retry carries a fresh token', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />
    )

    await user.type(screen.getByLabelText(/Your email/i), 'test@example.com')
    await user.type(screen.getByLabelText(/Your full name/i), 'John Doe')
    await user.type(
      screen.getByLabelText(/What would you like to talk about/i),
      'I need help with my career development in tech industry.'
    )
    await act(async () => {
      fireEvent.click(screen.getByTestId('turnstile'))
    })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send request/i }))
    })

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledTimes(1))
    expect(mockOnSubmit.mock.calls[0][0].captchaToken).toBe('mock-turnstile-token')

    // The page flips readyStatus loading -> error, which is what keys the reset.
    const fail = async (): Promise<void> => {
      rerender(<ContactMentorForm isLoading={true} isError={false} onSubmit={mockOnSubmit} />)
      rerender(<ContactMentorForm isLoading={false} isError={true} onSubmit={mockOnSubmit} />)
      // Let the widget hand back the replacement token.
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }

    await fail()
    expect(mockTurnstileReset).toHaveBeenCalledTimes(1)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send request/i }))
    })
    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledTimes(2))

    const retryToken = mockOnSubmit.mock.calls[1][0].captchaToken
    expect(retryToken).not.toBe('mock-turnstile-token')
    expect(retryToken).toMatch(/^fresh-turnstile-token-\d+$/)

    // Every failure has to reset, not just the first one.
    await fail()
    expect(mockTurnstileReset).toHaveBeenCalledTimes(2)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send request/i }))
    })
    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledTimes(3))
    expect(mockOnSubmit.mock.calls[2][0].captchaToken).not.toBe(retryToken)
  })

  it('leaves the captcha alone while no error is reported', async () => {
    const { rerender } = render(
      <ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />
    )

    rerender(<ContactMentorForm isLoading={true} isError={false} onSubmit={mockOnSubmit} />)
    rerender(<ContactMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(mockTurnstileReset).not.toHaveBeenCalled()
  })
})
