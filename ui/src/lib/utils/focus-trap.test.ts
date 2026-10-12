// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import { trapFocus } from './focus-trap'

function dialog(...labels: string[]): HTMLElement {
  const node = document.createElement('div')
  for (const label of labels) {
    const b = document.createElement('button')
    b.textContent = label
    node.appendChild(b)
  }
  document.body.appendChild(node)
  return node
}

describe('trapFocus', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('gives focus back to the opener when it closes', () => {
    const opener = dialog('Open').querySelector('button')!
    opener.focus()
    const trap = trapFocus(dialog('OK'))
    ;(document.querySelectorAll('button')[1] as HTMLElement).focus()
    trap.destroy()
    expect(document.activeElement).toBe(opener)
  })

  it('does not pull focus out of a dialog that opened while it was closing', () => {
    const opener = dialog('Open').querySelector('button')!
    opener.focus()
    const first = trapFocus(dialog('First'))
    const second = dialog('Second')
    second.querySelector('button')!.focus()
    first.destroy()
    expect(document.activeElement).toBe(second.querySelector('button'))
  })
})
