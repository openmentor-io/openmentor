import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import NoIndexHead from '@/components/layout/NoIndexHead'
import AuthGateScreen from '@/components/layout/AuthGateScreen'

// next/head drops its children in jsdom; render them inline so the emitted
// tags are assertable.
jest.mock('next/head', () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

// React 19 hoists <title>/<meta> out of the render container into document.head.
function robots(): string | null {
  return document.head.querySelector('meta[name="robots"]')?.getAttribute('content') ?? null
}

function title(): string | null {
  return document.head.querySelector('title')?.textContent ?? null
}

describe('NoIndexHead', () => {
  it('keeps the page out of the index and suffixes the title', () => {
    render(<NoIndexHead title="Requests" />)

    expect(robots()).toBe('noindex,follow')
    expect(title()).toBe('Requests | OpenMentor.io')
  })
})

describe('AuthGateScreen', () => {
  // The regression this guards: the directive used to live only in the
  // authenticated branch, so the server-rendered response a crawler gets --
  // which is always this gate, since auth resolves client-side -- carried none.
  it('carries the noindex directive on the pre-auth screen', () => {
    render(<AuthGateScreen title="Requests" />)

    expect(robots()).toBe('noindex,follow')
    expect(title()).toBe('Requests | OpenMentor.io')
  })
})
