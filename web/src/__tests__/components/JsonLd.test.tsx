import { render } from '@testing-library/react'
import JsonLd from '@/components/layout/JsonLd'

function scriptText(container: HTMLElement): string {
  const script = container.querySelector('script[type="application/ld+json"]')
  expect(script).not.toBeNull()
  return script?.innerHTML ?? ''
}

describe('JsonLd', () => {
  it('escapes markup so a payload cannot close the script tag', () => {
    const { container } = render(
      <JsonLd data={{ name: '</script><img src=x onerror=alert(1)>', note: 'a & b' }} />
    )
    const raw = scriptText(container)

    expect(raw).not.toContain('</script>')
    expect(raw).not.toContain('<img')
    expect(raw).not.toContain('&')
  })

  it('still round-trips to the original values through JSON.parse', () => {
    const data = { name: '</script>', note: 'a & b', tags: ['<Go>', 'C & C++'] }
    const { container } = render(<JsonLd data={data} />)

    expect(JSON.parse(scriptText(container))).toEqual(data)
  })
})
