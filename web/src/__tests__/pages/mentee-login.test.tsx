import { act, fireEvent, render, screen } from '@testing-library/react'
import MenteeLoginPage from '@/pages/mentee/login'

jest.mock('next/router', () => ({
  useRouter: () => ({ query: {}, replace: jest.fn(), isReady: true }),
}))
jest.mock('@/lib/mentee-api', () => ({
  getMenteeSession: jest.fn(async () => null),
  requestMenteeLogin: jest.fn(async () => ({ success: true })),
  verifyMenteeLogin: jest.fn(),
  logoutMentee: jest.fn(),
}))

describe('mentee login page', () => {
  // The input is masked by session replay by default; the address echoed back
  // in the "check your inbox" state is plain text and must be masked too.
  it('masks the echoed email address from session replay', async () => {
    render(<MenteeLoginPage />)
    const input = await screen.findByLabelText('Email')
    fireEvent.change(input, { target: { value: 'jane.mentee@example.com' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /magic link/i }))
    })

    const echoed = await screen.findByText('jane.mentee@example.com')
    expect(echoed.closest('.ph-mask')).not.toBeNull()
  })
})
