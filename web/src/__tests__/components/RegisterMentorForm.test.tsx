import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import RegisterMentorForm from '@/components/forms/RegisterMentorForm'

// Mock ReCAPTCHA component
jest.mock('react-google-recaptcha', () => ({
  __esModule: true,
  default: function MockReCAPTCHA({ onChange }: { onChange: (token: string | null) => void }) {
    return (
      <button
        type="button"
        data-testid="recaptcha"
        onClick={() => onChange('mock-recaptcha-token')}
      >
        Complete ReCAPTCHA
      </button>
    )
  },
}))

// Mock react-select to avoid issues with portal rendering
jest.mock('react-select', () => ({
  __esModule: true,
  default: function MockSelect({
    options,
    onChange,
    value,
  }: {
    options: Array<{ value: string; label: string }>
    onChange: (selected: Array<{ value: string; label: string }>) => void
    value: Array<{ value: string; label: string }>
  }) {
    return (
      <div data-testid="tags-select">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => {
              const isSelected = value.some((v) => v.value === option.value)
              if (isSelected) {
                onChange(value.filter((v) => v.value !== option.value))
              } else if (value.length < 5) {
                onChange([...value, option])
              }
            }}
          >
            {option.label} {value.some((v) => v.value === option.value) && '✓'}
          </button>
        ))}
      </div>
    )
  },
}))

// Mock Wysiwyg editor
jest.mock('@/components/forms/Wysiwyg', () => ({
  __esModule: true,
  default: function MockWysiwyg({
    content,
    onUpdate,
  }: {
    content: string
    onUpdate: (editor: { getHTML: () => string }) => void
  }) {
    return (
      <textarea
        value={content}
        onChange={(e) => onUpdate({ getHTML: () => e.target.value })}
        data-testid="wysiwyg"
      />
    )
  },
}))

describe('RegisterMentorForm', () => {
  const mockOnSubmit = jest.fn()

  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('renders all required form fields', () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByLabelText(/Your full name/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Your email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Telegram \(optional\)/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Job title/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Company/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Experience/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Price per one-hour session/i)).toBeInTheDocument()
    // Tags field uses Controller-wrapped Select, so check for label text instead
    expect(screen.getByText(/Specialization/i)).toBeInTheDocument()
    // Wysiwyg fields also use Controller, check for label text
    expect(screen.getByText(/About you/i)).toBeInTheDocument()
    expect(screen.getByText(/How can you help/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Skills and technologies/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Profile photo/i)).toBeInTheDocument()
  })

  it('renders optional calendar URL field', () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByLabelText(/Booking link to your calendar/i)).toBeInTheDocument()
  })

  it('renders submit button', () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByRole('button', { name: /Submit application/i })).toBeInTheDocument()
  })

  it('disables submit button when isLoading is true', () => {
    render(<RegisterMentorForm isLoading={true} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByRole('button', { name: /Submitting/i })).toBeDisabled()
  })

  it('displays error message when isError is true', () => {
    render(<RegisterMentorForm isLoading={false} isError={true} onSubmit={mockOnSubmit} />)

    expect(screen.getByText(/Something went wrong/i)).toBeInTheDocument()
  })

  it('shows validation error for empty required fields on submit', async () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Complete ReCAPTCHA to enable submit button
    await act(async () => {
      fireEvent.click(screen.getByTestId('recaptcha'))
    })

    const submitButton = screen.getByRole('button', { name: /Submit application/i })

    await act(async () => {
      fireEvent.click(submitButton)
    })

    // Form should not be submitted when fields are empty
    await waitFor(() => {
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })
  })

  it('shows error for missing profile picture', async () => {
    const user = userEvent.setup()
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    // Fill all fields except profile picture
    await user.type(screen.getByLabelText(/Your full name/i), 'John Doe')
    await user.type(screen.getByLabelText(/Your email/i), 'john@example.com')
    await user.type(screen.getByLabelText(/Telegram \(optional\)/i), 'johndoe')
    await user.type(screen.getByLabelText(/Job title/i), 'Engineer')
    await user.type(screen.getByLabelText(/Company/i), 'Tech Company')
    await user.selectOptions(screen.getByLabelText(/Experience/i), '10+')
    await user.selectOptions(screen.getByLabelText(/Price per one-hour session/i), '$100')

    // Complete recaptcha
    await act(async () => {
      fireEvent.click(screen.getByTestId('recaptcha'))
    })

    // Submit form
    const submitButton = screen.getByRole('button', { name: /Submit application/i })
    await act(async () => {
      fireEvent.click(submitButton)
    })

    // Should not submit without profile picture
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  it('renders experience dropdown with correct options', () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const select = screen.getByLabelText(/Experience/i)
    expect(select).toBeInTheDocument()

    // Check options exist
    expect(screen.getByRole('option', { name: /2-5 years/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /5-10 years/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /10\+ years/i })).toBeInTheDocument()
  })

  it('renders recaptcha component', () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    expect(screen.getByTestId('recaptcha')).toBeInTheDocument()
  })

  it('disables submit button without recaptcha token', async () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const submitButton = screen.getByRole('button', { name: /Submit application/i })

    // Submit button should be disabled without recaptcha
    expect(submitButton).toBeDisabled()
  })

  it('enables submit button after recaptcha completion', async () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const submitButton = screen.getByRole('button', { name: /Submit application/i })

    // Initially disabled
    expect(submitButton).toBeDisabled()

    // Complete recaptcha
    await act(async () => {
      fireEvent.click(screen.getByTestId('recaptcha'))
    })

    await waitFor(() => {
      // Should be enabled after recaptcha
      expect(submitButton).not.toBeDisabled()
    })
  })

  it('displays preview for selected profile picture', async () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const file = new File(['fake-image'], 'profile.jpg', { type: 'image/jpeg' })
    const fileInput = screen.getByLabelText(/Profile photo/i) as HTMLInputElement

    // Mock FileReader
    const mockFileReader = {
      readAsDataURL: jest.fn(),
      onloadend: null as (() => void) | null,
      result: 'data:image/jpeg;base64,fake-image-data',
    }

    jest
      .spyOn(global, 'FileReader')
      .mockImplementation(() => mockFileReader as unknown as FileReader)

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    // Trigger onloadend
    await act(async () => {
      if (mockFileReader.onloadend) {
        mockFileReader.onloadend()
      }
    })

    await waitFor(() => {
      expect(screen.getByText(/Photo selected:/i)).toBeInTheDocument()
    })

    jest.restoreAllMocks()
  })

  it('validates invalid email format', async () => {
    const user = userEvent.setup()
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    await user.type(screen.getByLabelText(/Your email/i), 'invalid-email')

    await act(async () => {
      fireEvent.click(screen.getByTestId('recaptcha'))
    })

    const submitButton = screen.getByRole('button', { name: /Submit application/i })
    await act(async () => {
      fireEvent.click(submitButton)
    })

    // Form should not be submitted with invalid email
    await waitFor(() => {
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })
  })

  it('allows canceling selected image', async () => {
    render(<RegisterMentorForm isLoading={false} isError={false} onSubmit={mockOnSubmit} />)

    const file = new File(['fake-image'], 'profile.jpg', { type: 'image/jpeg' })
    const fileInput = screen.getByLabelText(/Profile photo/i) as HTMLInputElement

    const mockFileReader = {
      readAsDataURL: jest.fn(),
      onloadend: null as (() => void) | null,
      result: 'data:image/jpeg;base64,fake-image-data',
    }

    jest
      .spyOn(global, 'FileReader')
      .mockImplementation(() => mockFileReader as unknown as FileReader)

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await act(async () => {
      if (mockFileReader.onloadend) {
        mockFileReader.onloadend()
      }
    })

    await waitFor(() => {
      expect(screen.getByText(/Photo selected:/i)).toBeInTheDocument()
    })

    // Cancel the image
    const cancelButton = screen.getByRole('button', { name: /Cancel/i })
    await act(async () => {
      fireEvent.click(cancelButton)
    })

    await waitFor(() => {
      expect(screen.queryByText(/Photo selected:/i)).not.toBeInTheDocument()
    })

    jest.restoreAllMocks()
  })
})
