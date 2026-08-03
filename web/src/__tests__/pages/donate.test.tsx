import { render, screen } from '@testing-library/react'
import Donate from '@/pages/donate'
import donates from '@/config/donates'

jest.mock('@/components', () => ({
  __esModule: true,
  NavHeader: () => <div data-testid="nav-header" />,
  Footer: () => <div data-testid="footer" />,
  MetaHeader: () => null,
}))

jest.mock('@/lib/analytics', () => ({
  __esModule: true,
  default: { event: jest.fn(), events: { DONATE_PAGE_VIEWED: 'v', DONATE_CTA_CLICKED: 'c' } },
}))

describe('Donate page', () => {
  it('sends donors to the configured Ko-fi URL', () => {
    render(<Donate />)

    expect(screen.getByRole('link', { name: /Donate on Ko-fi/i })).toHaveAttribute(
      'href',
      donates[0].linkUrl,
    )
  })

  // Ko-fi has no documented URL parameter for preselecting an amount, so an
  // on-page picker could only ever relabel the button. Guard against one
  // coming back.
  it('offers no amount picker, since the amount cannot be passed to Ko-fi', () => {
    render(<Donate />)

    expect(screen.queryByRole('group', { name: /amount/i })).not.toBeInTheDocument()
    for (const amount of ['$3', '$10', '$25', 'Custom']) {
      expect(screen.queryByRole('button', { name: amount })).not.toBeInTheDocument()
    }
    expect(screen.getByRole('link', { name: /Ko-fi/i })).toHaveTextContent(/^Donate on Ko-fi/)
  })
})
