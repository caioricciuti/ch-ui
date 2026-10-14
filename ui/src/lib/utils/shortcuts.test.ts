// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { tabShortcut } from './shortcuts'

function press(code: string, opts: { alt?: boolean; meta?: boolean; shift?: boolean; key?: string; target?: EventTarget | null } = {}) {
  return {
    key: opts.key ?? '',
    code,
    altKey: !!opts.alt,
    metaKey: !!opts.meta,
    ctrlKey: false,
    shiftKey: !!opts.shift,
    target: opts.target ?? document.body,
  }
}

describe('tabShortcut', () => {
  // jsdom has no isContentEditable; a textarea stands in for the editor.
  const editor = document.createElement('textarea')
  const input = document.createElement('input')

  it('maps Option shortcuts outside text fields', () => {
    expect(tabShortcut(press('KeyW', { alt: true, key: '∑' }))).toBe('close')
    expect(tabShortcut(press('KeyD', { alt: true, key: '∂' }))).toBe('duplicate')
    expect(tabShortcut(press('KeyS', { alt: true, key: 'ß' }))).toBe('split')
    expect(tabShortcut(press('F2', { key: 'F2' }))).toBe('rename')
  })

  it('leaves Option letters and F2 to the editor and inputs', () => {
    for (const target of [editor, input]) {
      expect(tabShortcut(press('KeyW', { alt: true, key: '∑', target }))).toBeNull()
      expect(tabShortcut(press('KeyD', { alt: true, key: '∂', target }))).toBeNull()
      expect(tabShortcut(press('KeyS', { alt: true, key: 'ß', target }))).toBeNull()
      expect(tabShortcut(press('F2', { key: 'F2', target }))).toBeNull()
    }
  })

  it('keeps the Mod-Shift forms working while typing', () => {
    expect(tabShortcut(press('KeyW', { meta: true, shift: true, key: 'W', target: editor }))).toBe('close')
    expect(tabShortcut(press('KeyD', { meta: true, shift: true, key: 'D', target: editor }))).toBe('duplicate')
  })

  it('ignores plain letters', () => {
    expect(tabShortcut(press('KeyW', { key: 'w' }))).toBeNull()
  })
})
