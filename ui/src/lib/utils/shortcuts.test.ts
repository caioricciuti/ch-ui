// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { isInModal, shortcutGroups, tabShortcut } from './shortcuts'

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

describe('isInModal', () => {
  it('is true inside an open sheet or dialog only', () => {
    const dialog = document.createElement('div')
    dialog.setAttribute('aria-modal', 'true')
    const inside = dialog.appendChild(document.createElement('button'))
    const outside = document.createElement('button')
    document.body.append(dialog, outside)
    expect(isInModal(inside)).toBe(true)
    expect(isInModal(outside)).toBe(false)
    expect(isInModal(null)).toBe(false)
  })

  it('keeps tab shortcuts away from a focused dialog', () => {
    const dialog = document.createElement('div')
    dialog.setAttribute('aria-modal', 'true')
    const button = dialog.appendChild(document.createElement('button'))
    document.body.appendChild(dialog)
    expect(tabShortcut(press('KeyW', { alt: true, key: '∑', target: button }))).toBeNull()
  })
})

describe('shortcutGroups', () => {
  it('lists each combo once per group', () => {
    for (const group of shortcutGroups()) {
      const seen = new Set<string>()
      for (const item of group.items) {
        const id = `${item.keys.join('+')} ${item.label}`
        expect(seen.has(id)).toBe(false)
        seen.add(id)
      }
    }
  })
})
