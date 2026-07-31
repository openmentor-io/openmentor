import { render, screen } from '@testing-library/react'
import StatusBadge from '@/components/mentor-admin/StatusBadge'
import type { RequestStatus } from '@/types'
import {
  ALL_REQUEST_STATUSES,
  STATUS_COLORS,
  STATUS_LABELS,
  STATUS_TRANSITIONS,
} from '@/types'

describe('request status maps', () => {
  // The database's client_requests_status_chk constraint is the source of
  // truth. A status it permits but the UI does not list reaches these maps as
  // undefined — which is how 'reschedule' crashed the admin request list.
  const statusesFromSchema: RequestStatus[] = [
    'pending',
    'contacted',
    'working',
    'done',
    'reschedule',
    'declined',
    'unavailable',
  ]

  it('lists every status the database CHECK constraint permits', () => {
    expect([...ALL_REQUEST_STATUSES].sort()).toEqual([...statusesFromSchema].sort())
  })

  it.each(statusesFromSchema)('has a label, colors and transitions for %s', (status) => {
    expect(STATUS_LABELS[status]).toBeTruthy()
    expect(STATUS_COLORS[status]).toBeDefined()
    expect(STATUS_COLORS[status].bg).toBeTruthy()
    expect(STATUS_COLORS[status].text).toBeTruthy()
    expect(STATUS_TRANSITIONS[status]).toBeDefined()
  })

  // 'reschedule' is admin-only: the mentor's inbox never showed it, and the Go
  // model's CanTransitionTo falls through to false for it.
  it('offers no mentor-side transitions out of or into reschedule', () => {
    expect(STATUS_TRANSITIONS.reschedule).toEqual([])
    for (const status of ALL_REQUEST_STATUSES) {
      expect(STATUS_TRANSITIONS[status]).not.toContain('reschedule')
    }
  })
})

describe('StatusBadge', () => {
  it.each(ALL_REQUEST_STATUSES)('renders the %s status', (status) => {
    render(<StatusBadge status={status} />)
    expect(screen.getByText(STATUS_LABELS[status])).toBeInTheDocument()
  })

  it('renders reschedule with its own pill colors', () => {
    const { container } = render(<StatusBadge status="reschedule" />)
    const badge = container.querySelector('span')
    expect(badge).toHaveTextContent('Reschedule')
    expect(badge?.className).toContain(STATUS_COLORS.reschedule.bg)
  })

  // Status arrives as JSON, so the type is no runtime guarantee. An unknown
  // value must degrade to a neutral pill, never crash the page.
  it('falls back to a neutral pill for an unknown status', () => {
    const unknown = 'archived' as RequestStatus
    const { container } = render(<StatusBadge status={unknown} />)
    const badge = container.querySelector('span')
    expect(badge).toHaveTextContent('archived')
    expect(badge?.className).toContain('bg-surface-deep')
  })
})
