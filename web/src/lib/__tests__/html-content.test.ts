import { htmlContent } from '@/lib/html-content'

describe('htmlContent', () => {
  it('wraps plain text with paragraphs and preserves line breaks', () => {
    const raw = 'First line\nSecond line'
    const result = htmlContent(raw)

    expect(result).toContain('<p>First line</p>')
    expect(result).toContain('<p>Second line</p>')
  })

  it('turns a newline run before a list marker into a line break', () => {
    expect(htmlContent('Steps\n1. first\n\n- second')).toBe(
      '<p>Steps<br />1. first<br />- second</p>'
    )
  })

  it('stays linear on a long run of newlines', () => {
    const raw = '\n'.repeat(100_000) + 'a'

    const start = Date.now()
    const result = htmlContent(raw)
    const elapsed = Date.now() - start

    expect(result).toBe('<p></p><p>a</p>')
    // The backtracking version needed ~11s for this input.
    expect(elapsed).toBeLessThan(2000)
  })

  it('sanitizes script tags', () => {
    const raw = '<p>Hello</p><script>alert("xss")</script>'
    const result = htmlContent(raw)

    expect(result).toContain('<p>Hello</p>')
    expect(result).not.toContain('<script>')
  })

  it('forces safe link attributes', () => {
    const raw = '<a href="http://example.com">Example</a>'
    const result = htmlContent(raw)

    expect(result).toContain('target="_blank"')
    expect(result).toContain('rel="noopener noreferrer"')
  })
})
