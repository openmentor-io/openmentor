import { isValidCalendarUrl, safeHttpUrl } from '@/lib/safe-url'

describe('isValidCalendarUrl', () => {
  // These are the same values api/test/internal/models/
  // calendar_url_validation_test.go binds against the https_url tag. The two
  // must agree: anything accepted here that the tag rejects returns a 400 that
  // names no field, which the calendar-link forms can only show as a generic
  // error.
  it.each([
    ['', true],
    [undefined, true],
    ['https://calendly.com/jane', true],
    ['https://cal.com/x?a=1&b=2', true],
    ['http://calendly.com/jane', false],
    ['javascript:alert(1)', false],
    ['data:text/html,<script>alert(1)</script>', false],
    ['mailto:jane@example.com', false],
    ['not-a-url', false],
    ['https://', false],
    // new URL() tolerates these, the API tag does not.
    ['https://calendly.com/jane ', false],
    [' https://calendly.com/jane', false],
    ['https://user:pass@calendly.com/x', false],
    ['https://calendly.com/x">', false],
  ])('isValidCalendarUrl(%p) === %p', (value, expected) => {
    expect(isValidCalendarUrl(value)).toBe(expected)
  })
})

describe('safeHttpUrl', () => {
  it('keeps stored http links renderable', () => {
    // Deliberately looser than isValidCalendarUrl: rows predating the
    // https_url tag must keep rendering.
    expect(safeHttpUrl('http://calendly.com/jane')).toBe('http://calendly.com/jane')
  })

  it('rejects dangerous schemes', () => {
    expect(safeHttpUrl('javascript:alert(1)')).toBeNull()
    expect(safeHttpUrl(null)).toBeNull()
  })
})
