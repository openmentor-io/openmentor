import { render, screen, fireEvent, act } from '@testing-library/react'
import ProfileForm from '@/components/forms/ProfileForm'
import type { MentorWithSecureFields } from '@/types'

// Mock react-select to avoid portal rendering; tags come from the fixture.
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

const mentor: MentorWithSecureFields = {
  id: 1,
  mentorId: 'm1',
  slug: 'jane',
  name: 'Jane Mentor',
  job: 'Staff Engineer',
  workplace: 'Acme',
  description: 'I can help with backend design.',
  about: 'Ten years of backend work.',
  competencies: 'Go, Postgres',
  experience: '10+',
  price: 'Free',
  tags: ['Backend'],
  menteeCount: 3,
  photo_url: null,
  sortOrder: 1,
  isVisible: true,
  isNew: false,
  calendarType: 'url',
  calendarUrl: 'https://calendly.com/jane',
}

describe('ProfileForm calendar URL', () => {
  const onSubmit = jest.fn()
  const onImageUpload = jest.fn()

  const renderForm = (calendarUrl: string | null): void => {
    render(
      <ProfileForm
        mentor={{ ...mentor, calendarUrl }}
        isLoading={false}
        isError={false}
        onSubmit={onSubmit}
        onImageUpload={onImageUpload}
        imageUploadStatus="idle"
        tempImagePreview={null}
      />
    )
  }

  const save = async (): Promise<void> => {
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Save/i }))
    })
  }

  beforeEach(() => {
    jest.clearAllMocks()
  })

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
    renderForm('')
    fireEvent.change(screen.getByLabelText(/Booking link to your calendar/i), {
      target: { value },
    })
    await save()

    const error = screen.queryByText(/must be a valid https:\/\/ URL/i)
    if (accepted) {
      expect(error).not.toBeInTheDocument()
      expect(onSubmit).toHaveBeenCalled()
    } else {
      expect(error).toBeInTheDocument()
      expect(onSubmit).not.toHaveBeenCalled()
    }
  })

  it('trims a pasted booking link instead of failing the save', async () => {
    renderForm('')
    fireEvent.change(screen.getByLabelText(/Booking link to your calendar/i), {
      target: { value: '  https://calendly.com/jane  ' },
    })
    await save()

    expect(screen.queryByText(/must be a valid https:\/\/ URL/i)).not.toBeInTheDocument()
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ calendarUrl: 'https://calendly.com/jane' })
    )
  })
})
