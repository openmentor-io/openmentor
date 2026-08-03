import { render, screen, waitFor } from '@testing-library/react'
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

function priceSelect(): HTMLSelectElement {
  return screen.getByLabelText(/Price per one-hour session/i) as HTMLSelectElement
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

  // `price` is a free-text column (DECISIONS D3) and these are only
  // suggestions, so a stored value outside the list must survive an edit to
  // any other field. It used to be reported as the first option, 'Free'.
  it.each(['$75', '$30 / hour', '$5', '$125'])(
    'keeps a stored price outside the suggestion list: %s',
    async (price) => {
      renderForm({ price })

      expect(priceSelect().value).toBe(price)
      expect(screen.getByRole('option', { name: price, selected: true })).toBeInTheDocument()

      await editNameAndSave()

      expect(mockOnSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'Jane R. Doe', price })
      )
    }
  )

  it('round-trips a price that is one of the suggestions', async () => {
    renderForm({ price: '$100' })

    expect(priceSelect().value).toBe('$100')
    // No duplicate option was injected for a value already on the list.
    expect(screen.getAllByRole('option', { name: '$100' })).toHaveLength(1)

    await editNameAndSave()

    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ price: '$100' }))
  })

  it('still lets the mentor pick a suggested price', async () => {
    const user = userEvent.setup()
    renderForm({ price: '$75' })

    await user.selectOptions(priceSelect(), 'Negotiable')
    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
    expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ price: 'Negotiable' }))
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

    expect(priceSelect().value).toBe('')
    expect(screen.getByRole('option', { name: /Please choose one/i })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Save/i }))

    await waitFor(() =>
      expect(screen.getAllByText(/This field is required/i).length).toBeGreaterThan(0)
    )
    expect(mockOnSubmit).not.toHaveBeenCalled()
  })

  // Blocking the save is not a lockout: `experience` is `binding:"required"` on
  // SaveProfileRequest, so forwarding the empty string would come back a 400.
  // What matters is that the mentor can clear it in one step — the field is
  // focused and flagged, and choosing a value saves.
  it.each(['experience', 'price'])(
    'lets a mentor whose %s was never set save after choosing one',
    async (field) => {
      const user = userEvent.setup()
      const label = field === 'price' ? /Price per one-hour session/i : /Experience/i
      const chosen = field === 'price' ? 'Negotiable' : '5-10'
      renderForm({ [field]: '' })

      const select = screen.getByLabelText(label) as HTMLSelectElement
      expect(select.value).toBe('')

      await user.click(screen.getByRole('button', { name: /Save/i }))

      await waitFor(() => expect(screen.getByText(/This field is required/i)).toBeInTheDocument())
      expect(mockOnSubmit).not.toHaveBeenCalled()
      // react-hook-form focuses the first invalid field, which scrolls the
      // message into view rather than leaving Save looking inert.
      expect(select).toHaveFocus()

      await user.selectOptions(select, chosen)
      await user.click(screen.getByRole('button', { name: /Save/i }))

      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
      expect(mockOnSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ [field]: chosen, name: 'Jane Doe' })
      )
    }
  )
})
