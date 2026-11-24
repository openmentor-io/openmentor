import { render, screen, fireEvent } from '@testing-library/react'
import NavHeader from '@/components/layout/NavHeader'

describe('NavHeader login menu', () => {
  it('offers both login realms from the desktop "Log in" menu', () => {
    render(<NavHeader />)
    fireEvent.click(screen.getByRole('button', { name: /log in/i }))
    expect(screen.getByRole('menuitem', { name: 'Log in as mentor' })).toHaveAttribute(
      'href',
      '/mentor/login',
    )
    expect(screen.getByRole('menuitem', { name: 'Log in as mentee' })).toHaveAttribute(
      'href',
      '/mentee/login',
    )
  })

  it('lists both login realms in the mobile menu', () => {
    render(<NavHeader />)
    fireEvent.click(screen.getByRole('button', { name: /open menu/i }))
    const links = screen
      .getAllByRole('link', { name: /log in as/i })
      .map((l) => l.getAttribute('href'))
    expect(links).toEqual(['/mentor/login', '/mentee/login'])
  })
})
