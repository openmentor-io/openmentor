import { act, render, screen, waitFor } from '@testing-library/react'
import { MenteeAuthProvider, useMenteeAuth } from '@/components/mentee'
import analytics from '@/lib/analytics'

jest.mock('@/lib/mentee-api', () => ({
  getMenteeSession: jest.fn(async () => ({ mentee_id: 'mentee-1', exp: 9e9, iat: 0 })),
  logoutMentee: jest.fn(async () => undefined),
  requestMenteeLogin: jest.fn(),
  verifyMenteeLogin: jest.fn(),
}))

jest.mock('@/lib/analytics', () => ({
  __esModule: true,
  default: {
    events: { MENTEE_AUTH_LOGOUT: 'mentee_auth_logout' },
    event: jest.fn(),
    identify: jest.fn(),
    reset: jest.fn(),
  },
}))

function LogoutButton(): JSX.Element {
  const { isAuthenticated, logout } = useMenteeAuth()
  return <button onClick={() => void logout()}>{isAuthenticated ? 'in' : 'out'}</button>
}

describe('MenteeAuthProvider analytics identity', () => {
  // Otherwise everything done in this browser after logout is attributed to
  // the previous mentee.
  it('identifies the mentee, then resets the identity on logout', async () => {
    render(
      <MenteeAuthProvider>
        <LogoutButton />
      </MenteeAuthProvider>,
    )
    await waitFor(() => expect(screen.getByRole('button')).toHaveTextContent('in'))
    expect(analytics.identify).toHaveBeenCalledWith('mentee:mentee-1', { role: 'mentee' })
    expect(analytics.reset).not.toHaveBeenCalled()

    await act(async () => {
      screen.getByRole('button').click()
    })

    expect(screen.getByRole('button')).toHaveTextContent('out')
    expect(analytics.reset).toHaveBeenCalled()
  })
})
