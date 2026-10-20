/**
 * Keyboard rules shared by Sheet, Modal and the popovers that open inside
 * them. Esc closes only the topmost open layer, so Esc in a combobox inside
 * a sheet closes the combobox, not the sheet. Enter in a single-line field
 * runs the footer's primary action.
 */

export interface Layer {
  /** True while nothing opened on top of this layer. */
  isTop(): boolean
  /** Removes the layer; call when it closes. */
  release(): void
}

const layers: symbol[] = []

/** Registers an open overlay. The newest one is on top. */
export function openLayer(): Layer {
  const id = Symbol('layer')
  layers.push(id)
  return {
    isTop: () => layers[layers.length - 1] === id,
    release: () => {
      const i = layers.indexOf(id)
      if (i >= 0) layers.splice(i, 1)
    },
  }
}

/**
 * Focus is in a code editor (CodeMirror or another contenteditable). Esc
 * there leaves the editor first instead of closing the dialog, so a stray
 * Esc never throws away unsaved SQL.
 */
export function isEditorTarget(target: EventTarget | null): boolean {
  if (typeof Element === 'undefined' || !(target instanceof Element)) return false
  return target.closest('.cm-editor, [contenteditable="true"], [contenteditable=""]') !== null
}

/** Input types that hold one line of text, where Enter has no meaning of its own. */
const SINGLE_LINE = new Set(['', 'text', 'email', 'password', 'search', 'url', 'tel', 'number'])

type EnterEvent = Pick<
  KeyboardEvent,
  'key' | 'shiftKey' | 'altKey' | 'metaKey' | 'ctrlKey' | 'isComposing' | 'defaultPrevented' | 'target'
>

/**
 * Enter in a single-line input should run the dialog's primary action.
 * Not in textareas or code editors (Enter is a newline there), not while an
 * IME is composing, not when something already handled the key, and not
 * inside a <form>: the browser submits that form on its own.
 */
export function shouldSubmitOnEnter(e: EnterEvent): boolean {
  if (e.key !== 'Enter' || e.defaultPrevented || e.isComposing) return false
  if (e.shiftKey || e.altKey || e.metaKey || e.ctrlKey) return false
  if (typeof HTMLInputElement === 'undefined' || !(e.target instanceof HTMLInputElement)) return false
  const input = e.target
  if (!SINGLE_LINE.has(input.getAttribute('type')?.toLowerCase() ?? '')) return false
  if (input.form) return false
  if (input.getAttribute('role') === 'combobox') return false
  return true
}

/**
 * The enabled primary button in a dialog footer, or null. A disabled one
 * means the form is not valid yet, so Enter does nothing. Danger buttons
 * are never picked: a destructive action needs a deliberate click.
 */
export function primaryAction(footer: HTMLElement | null | undefined): HTMLButtonElement | null {
  if (!footer) return null
  const buttons = footer.querySelectorAll<HTMLButtonElement>('button[data-variant="primary"]')
  const last = buttons[buttons.length - 1]
  return last && !last.disabled ? last : null
}

/**
 * The element a focus trap moves to when Tab leaves either end, or null
 * when focus is somewhere in the middle and the browser can move it.
 */
export function wrapTarget(items: HTMLElement[], active: Element | null, shift: boolean, container: HTMLElement): HTMLElement | null {
  if (items.length === 0) return null
  const first = items[0]
  const last = items[items.length - 1]
  // The container itself counts as outside: Shift+Tab from it would leave.
  const outside = !active || active === container || !container.contains(active)
  if (shift && (active === first || outside)) return last
  if (!shift && (active === last || outside)) return first
  return null
}
