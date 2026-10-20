// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { isEditorTarget, openLayer, primaryAction, shouldSubmitOnEnter, wrapTarget } from './dialog-keys'

function enter(target: EventTarget | null, opts: Partial<Record<'shiftKey' | 'altKey' | 'metaKey' | 'ctrlKey' | 'isComposing' | 'defaultPrevented', boolean>> & { key?: string } = {}) {
  return {
    key: opts.key ?? 'Enter',
    shiftKey: !!opts.shiftKey,
    altKey: !!opts.altKey,
    metaKey: !!opts.metaKey,
    ctrlKey: !!opts.ctrlKey,
    isComposing: !!opts.isComposing,
    defaultPrevented: !!opts.defaultPrevented,
    target,
  }
}

function input(type?: string): HTMLInputElement {
  const el = document.createElement('input')
  if (type !== undefined) el.type = type
  document.body.appendChild(el)
  return el
}

beforeEach(() => {
  document.body.innerHTML = ''
})

describe('openLayer', () => {
  it('treats the newest open layer as the top one', () => {
    const sheet = openLayer()
    expect(sheet.isTop()).toBe(true)
    const combobox = openLayer()
    expect(sheet.isTop()).toBe(false)
    expect(combobox.isTop()).toBe(true)
    combobox.release()
    expect(sheet.isTop()).toBe(true)
    sheet.release()
  })

  it('handles layers released out of order', () => {
    const a = openLayer()
    const b = openLayer()
    const c = openLayer()
    b.release()
    expect(c.isTop()).toBe(true)
    c.release()
    expect(a.isTop()).toBe(true)
    a.release()
    a.release()
  })
})

describe('shouldSubmitOnEnter', () => {
  it('submits from single-line text fields', () => {
    for (const type of [undefined, 'text', 'email', 'password', 'search', 'url', 'number']) {
      expect(shouldSubmitOnEnter(enter(input(type)))).toBe(true)
    }
  })

  it('leaves textareas, editors, buttons and checkboxes alone', () => {
    const textarea = document.createElement('textarea')
    const editor = document.createElement('div')
    editor.contentEditable = 'true'
    const button = document.createElement('button')
    for (const target of [textarea, editor, button, input('checkbox'), input('radio'), input('file')]) {
      expect(shouldSubmitOnEnter(enter(target))).toBe(false)
    }
  })

  it('ignores modified, composing and handled presses', () => {
    const el = input('text')
    expect(shouldSubmitOnEnter(enter(el, { shiftKey: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { metaKey: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { ctrlKey: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { altKey: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { isComposing: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { defaultPrevented: true }))).toBe(false)
    expect(shouldSubmitOnEnter(enter(el, { key: 'a' }))).toBe(false)
  })

  it('leaves fields inside a form to the browser', () => {
    const form = document.createElement('form')
    const el = document.createElement('input')
    form.appendChild(el)
    document.body.appendChild(form)
    expect(shouldSubmitOnEnter(enter(el))).toBe(false)
  })

  it('leaves combobox search fields to the combobox', () => {
    const el = input('text')
    el.setAttribute('role', 'combobox')
    expect(shouldSubmitOnEnter(enter(el))).toBe(false)
  })
})

describe('primaryAction', () => {
  function footer(...buttons: Array<{ variant: string; disabled?: boolean }>): HTMLElement {
    const node = document.createElement('div')
    for (const b of buttons) {
      const el = document.createElement('button')
      el.dataset.variant = b.variant
      el.disabled = !!b.disabled
      node.appendChild(el)
    }
    return node
  }

  it('picks the last primary button', () => {
    const node = footer({ variant: 'outline' }, { variant: 'primary' })
    expect(primaryAction(node)).toBe(node.children[1])
  })

  it('returns nothing while the primary button is disabled', () => {
    expect(primaryAction(footer({ variant: 'outline' }, { variant: 'primary', disabled: true }))).toBeNull()
  })

  it('never picks a danger button', () => {
    expect(primaryAction(footer({ variant: 'outline' }, { variant: 'danger' }))).toBeNull()
  })

  it('handles a missing footer', () => {
    expect(primaryAction(undefined)).toBeNull()
  })
})

describe('wrapTarget', () => {
  function dialog(count: number) {
    const node = document.createElement('div')
    const items = Array.from({ length: count }, () => node.appendChild(document.createElement('button')))
    document.body.appendChild(node)
    return { node, items }
  }

  it('wraps Tab from the last item to the first and Shift+Tab back', () => {
    const { node, items } = dialog(3)
    expect(wrapTarget(items, items[2], false, node)).toBe(items[0])
    expect(wrapTarget(items, items[0], true, node)).toBe(items[2])
  })

  it('lets the browser move focus between middle items', () => {
    const { node, items } = dialog(3)
    expect(wrapTarget(items, items[1], false, node)).toBeNull()
    expect(wrapTarget(items, items[1], true, node)).toBeNull()
  })

  it('pulls focus back in from outside or from the dialog itself', () => {
    const { node, items } = dialog(2)
    const outside = document.body.appendChild(document.createElement('button'))
    expect(wrapTarget(items, outside, false, node)).toBe(items[0])
    expect(wrapTarget(items, node, true, node)).toBe(items[1])
    expect(wrapTarget(items, null, false, node)).toBe(items[0])
  })

  it('returns nothing when there is nothing to focus', () => {
    const { node } = dialog(0)
    expect(wrapTarget([], null, false, node)).toBeNull()
  })
})

describe('isEditorTarget', () => {
  it('is true inside a CodeMirror editor or a contenteditable', () => {
    const cm = document.createElement('div')
    cm.className = 'cm-editor'
    const line = cm.appendChild(document.createElement('div'))
    const editable = document.createElement('div')
    editable.setAttribute('contenteditable', 'true')
    document.body.append(cm, editable)
    expect(isEditorTarget(line)).toBe(true)
    expect(isEditorTarget(editable)).toBe(true)
  })

  it('is false for plain fields and buttons', () => {
    expect(isEditorTarget(input('text'))).toBe(false)
    expect(isEditorTarget(document.body.appendChild(document.createElement('textarea')))).toBe(false)
    expect(isEditorTarget(document.body.appendChild(document.createElement('button')))).toBe(false)
    expect(isEditorTarget(null)).toBe(false)
  })
})
