// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './brain-markdown'

describe('markdown with the real DOMPurify sanitizer', () => {
  it.each([
    '<script>alert(1)</script><b>safe</b>',
    '<img src=x onerror="alert(1)">',
    '<a href="javascript:alert(1)">click</a>',
    '<svg><g onload="alert(1)"></g></svg>',
    '<iframe srcdoc="<script>alert(1)</script>"></iframe>',
    '<math><mtext><table><mglyph><style><!--</style><img title="--><img src=x onerror=alert(1)>">',
  ])('removes active content from %s', (input) => {
    const root = document.createElement('div')
    root.innerHTML = renderMarkdown(input)
    expect(root.querySelector('script, iframe, svg, math')).toBeNull()
    for (const element of root.querySelectorAll('*')) {
      for (const attr of element.attributes) {
        expect(attr.name).not.toMatch(/^on/i)
        if (['href', 'src', 'xlink:href'].includes(attr.name)) {
          expect(attr.value).not.toMatch(/^\s*(javascript|vbscript):/i)
        }
      }
    }
  })

  it('preserves formatting and protects external links', () => {
    const root = document.createElement('div')
    root.innerHTML = renderMarkdown('**Safe** [docs](https://example.com/docs)')
    expect(root.querySelector('strong')?.textContent).toBe('Safe')
    const link = root.querySelector('a')!
    expect(link.getAttribute('href')).toBe('https://example.com/docs')
    expect(link.target).toBe('_blank')
    expect(link.rel).toBe('noopener noreferrer nofollow')
  })
})
