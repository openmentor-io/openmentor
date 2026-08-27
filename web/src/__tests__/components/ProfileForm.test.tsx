import { render, screen, fireEvent, act, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ProfileForm from '@/components/forms/ProfileForm'
import type { MentorWithSecureFields } from '@/types'

// Mock react-select: the tags field is irrelevant here and its portal
// rendering does not work under jsdom.
jest.mock('react-select', () => ({
  __esModule: true,
  default: function MockSelect() {
    return <div data-testid="tags-select" />
  },
}))

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

const baseMentor: MentorWithSecureFields = {
  id: 1,
  mentorId: 'aa63ba98-0000-4000-8000-000000000001',
  slug: 'jane-doe',
  name: 'Jane Doe',
  job: 'Staff Engineer',
  workplace: 'Acme',
  description: '<p>I can help with Go</p>',
  about: '<p>15 years of backend work</p>',
  competencies: 'Go, PostgreSQL',
  experience: '10+',
  price: '$100',
  tags: ['Backend'],
  menteeCount: 3,
  photo_url: null,
  sortOrder: 0,
  isVisible: true,
  isNew: false,
  calendarType: 'none',
  calendarUrl: null,
}

const mockOnSubmit = jest.fn()

function renderForm(overrides: Partial<MentorWithSecureFields> = {}): void {
  render(
    <ProfileForm
      mentor={{ ...baseMentor, ...overrides }}
      isLoading={false}
      isError={false}
      onSubmit={mockOnSubmit}
      onImageUpload={jest.fn()}
      imageUploadStatus="idle"
      tempImagePreview={null}
    />
  )
}

function pricePill(name: RegExp): HTMLInputElement {
  return screen.getByRole('radio', { name }) as HTMLInputElement
}

function amountBox(): HTMLInputElement {
  return screen.getByLabelText(/Price in US dollars/i) as HTMLInputElement
}

/** Edit an unrelated field and save — the path that used to destroy the price. */
async function editNameAndSave(): Promise<void> {
  const user = userEvent.setup()
  const name = screen.getByLabelText(/Your full name/i)
  await user.clear(name)
  await user.type(name, 'Jane R. Doe')
  await user.click(screen.getByRole('button', { name: /Save/i }))
  await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
}

describe('ProfileForm', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  // D44 in its new form. The old <select> reported its FIRST option when the
  // stored value matched none, silently rewriting a mentor's price to 'Free' on
  // an unrelated save. There is no first option to fall into any more, but the
  // invariant is the same: a stored price survives an edit to another field.
  it.each(['$75', '$5', '$125', '$1', '$1000', 'Free', 'Negotiable'])(
    'keeps the stored price %s across an unrelated edit',
    async (price) => {
      renderForm({ price })

      await editNameAndSave()

      expect(mockOnSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'Jane R. Doe', price })
      )
    }
  )

  it('prefills the amount pill and box from a stored fixed price', async () => {
    renderForm({ price: '$75' })

    expect(pricePill(/Fixed amount, \$75/i)).toBeChecked()
    expect(amountBox().value).toBe('75')
    expect(pricePill(/^Free$/i)).not.toBeChecked()
  })

  it('lets the mentor switch to Negotiable', async () => {
    const user = userEvent.setup()
    renderForm({ price: '$75' })

    await user.click(pricePill(/^Negotiable$/i))
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ price: 'Negotiable' }))
  })

  it('lets the mentor type an amount the old dropdown never offered', async () => {
    const user = userEvent.setup()
    renderForm({ price: 'Free' })

    await user.click(pricePill(/Set a fixed amount/i))
    await user.type(amountBox(), '137')
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ price: '$137' }))
  })

  // Switching away from the amount must drop it, or the form submits
  // {kind: 'free', amount: 137} — which mentors_price_chk rejects as a 500 from
  // the repository instead of as a field error the mentor can act on.
  it('drops a typed amount when the mentor switches to Free', async () => {
    const user = userEvent.setup()
    renderForm({ price: 'Negotiable' })

    await user.click(pricePill(/Set a fixed amount/i))
    await user.type(amountBox(), '137')
    await user.click(pricePill(/^Free$/i))
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ price: 'Free' }))
  })

  it.each(['$0', '$1001', '12345'])('refuses to save the out-of-range amount %s', async (typed) => {
    const user = userEvent.setup()
    renderForm({ price: 'Free' })

    await user.click(pricePill(/Set a fixed amount/i))
    await user.type(amountBox(), typed)
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/whole amount from \$1 to \$1000/i)
    )
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  // A value stored before the grammar existed leaves the control unselected
  // rather than being coerced into a real price — the same refusal to invent a
  // price that D44's 'Please choose one' placeholder made.
  it('leaves a stored price outside the grammar unselected and blocks the save', async () => {
    const user = userEvent.setup()
    renderForm({ price: '$30 / hour' })

    expect(pricePill(/^Free$/i)).not.toBeChecked()
    expect(pricePill(/^Negotiable$/i)).not.toBeChecked()
    expect(pricePill(/Set a fixed amount/i)).not.toBeChecked()

    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/whole amount from \$1 to \$1000/i)
    )
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  // Same mechanism, same TEXT column: the option set is closed in the UI only.
  it('keeps a stored experience outside the suggestion list', async () => {
    renderForm({ experience: '20+' })

    const experience = screen.getByLabelText(/Experience/i) as HTMLSelectElement
    expect(experience.value).toBe('20+')

    await editNameAndSave()

    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ experience: '20+' }))
  })

  it('round-trips an experience that is one of the suggestions', async () => {
    renderForm({ experience: '5-10' })

    expect((screen.getByLabelText(/Experience/i) as HTMLSelectElement).value).toBe('5-10')

    await editNameAndSave()

    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ experience: '5-10' }))
  })

  // The Go API rejects an empty price/experience (both `required`), so an unset
  // column must be caught here instead of being papered over with a suggestion.
  it('refuses to save an unset price instead of inventing one', async () => {
    const user = userEvent.setup()
    renderForm({ price: '' })

    expect(pricePill(/^Free$/i)).not.toBeChecked()

    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/whole amount from \$1 to \$1000/i)
    )
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  // Blocking the save is not a lockout: `experience` is `binding:"required"` on
  // SaveProfileRequest, so forwarding the empty string would come back a 400.
  // What matters is that the mentor can clear it in one step — the field is
  // focused and flagged, and choosing a value saves.
  it('lets a mentor whose experience was never set save after choosing one', async () => {
    const user = userEvent.setup()
    renderForm({ experience: '' })

    const select = screen.getByLabelText(/Experience/i) as HTMLSelectElement
    expect(select.value).toBe('')

    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(screen.getByText(/This field is required/i)).toBeInTheDocument())
    expect(mockOnSubmit).not.toHaveBeenCalled()
    // react-hook-form focuses the first invalid field, which scrolls the
    // message into view rather than leaving Save looking inert.
    expect(select).toHaveFocus()

    await user.selectOptions(select, '5-10')
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ experience: '5-10', name: 'Jane Doe' })
    )
  })

  // Same guarantee for price. The focus assertion is why PriceField forwards a
  // ref to its first pill: a <Controller> with no ref cannot be focused, and the
  // error would sit off-screen with Save looking inert.
  it('lets a mentor whose price was never set save after choosing one', async () => {
    const user = userEvent.setup()
    renderForm({ price: '' })

    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(mockOnSubmit).not.toHaveBeenCalled()
    expect(pricePill(/^Free$/i)).toHaveFocus()

    await user.click(pricePill(/^Negotiable$/i))
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ price: 'Negotiable', name: 'Jane Doe' })
    )
  })
})

describe('ProfileForm calendar URL', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  const renderCalendarForm = (calendarUrl: string | null): void => {
    renderForm({ calendarType: 'url', calendarUrl })
  }

  const save = async (): Promise<void> => {
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Save/i }))
    })
  }

  // A value the API's https_url tag rejects has to fail here: the save
  // endpoint's 400 names no field, so mentor/profile/edit.tsx can only show
  // "Something went wrong", and every retry fails identically.
  it.each([
    ['http://calendly.com/jane', false],
    ['javascript:alert(1)', false],
    ['https://user:pass@calendly.com/jane', false],
    ['not-a-url', false],
    ['', true],
    ['https://calendly.com/jane', true],
  ])('submits %s: %s', async (value, accepted) => {
    renderCalendarForm('')
    fireEvent.change(screen.getByLabelText(/Booking link to your calendar/i), {
      target: { value },
    })
    await save()

    const error = screen.queryByText(/must be a valid https:\/\/ URL/i)
    if (accepted) {
      expect(error).not.toBeInTheDocument()
      expect(mockOnSubmit).toHaveBeenCalled()
    } else {
      expect(error).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    }
  })

  it('trims a pasted booking link instead of failing the save', async () => {
    renderCalendarForm('')
    fireEvent.change(screen.getByLabelText(/Booking link to your calendar/i), {
      target: { value: '  https://calendly.com/jane  ' },
    })
    await save()

    expect(screen.queryByText(/must be a valid https:\/\/ URL/i)).not.toBeInTheDocument()
    expect(mockOnSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ calendarUrl: 'https://calendly.com/jane' })
    )
  })
})
