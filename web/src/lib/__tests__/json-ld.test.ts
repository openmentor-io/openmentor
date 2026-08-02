import { jsonLdScriptProps } from '@/lib/json-ld'

describe('jsonLdScriptProps', () => {
  it('marks the script as JSON-LD', () => {
    expect(jsonLdScriptProps({ '@type': 'Person' }).type).toBe('application/ld+json')
  })

  it('escapes markup so a payload cannot close the script tag', () => {
    const { dangerouslySetInnerHTML } = jsonLdScriptProps({
      name: '</script><img src=x onerror=alert(1)>',
      note: 'a & b',
    })

    expect(dangerouslySetInnerHTML.__html).not.toContain('</script>')
    expect(dangerouslySetInnerHTML.__html).not.toContain('<img')
    expect(dangerouslySetInnerHTML.__html).not.toContain('&')
  })

  it('still round-trips to the original values through JSON.parse', () => {
    const data = { name: '</script>', note: 'a & b', tags: ['<Go>', 'C & C++'] }

    expect(JSON.parse(jsonLdScriptProps(data).dangerouslySetInnerHTML.__html)).toEqual(data)
  })
})
