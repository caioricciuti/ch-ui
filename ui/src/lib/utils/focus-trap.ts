import { wrapTarget } from './dialog-keys'

/**
 * Svelte action for dialogs: keeps Tab inside the node, focuses the first
 * focusable element (or the node) on mount, and returns focus to whatever
 * had it when the dialog closes.
 */
export const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [contenteditable="true"], [tabindex]:not([tabindex="-1"])'

// Safari does not focus a button on click, so a dialog opened by a click
// would find the body focused and have nothing to give focus back to. The
// last pressed control stands in for the opener in that case.
let lastPressed: HTMLElement | null = null
if (typeof document !== 'undefined') {
  document.addEventListener(
    'pointerdown',
    (e) => {
      const target = e.target instanceof Element ? e.target.closest<HTMLElement>(FOCUSABLE) : null
      lastPressed = target
    },
    true,
  )
}

function opener(): HTMLElement | null {
  const active = document.activeElement as HTMLElement | null
  if (active && active !== document.body) return active
  return lastPressed && document.contains(lastPressed) ? lastPressed : null
}

export function trapFocus(node: HTMLElement, options: { initial?: 'first' | 'none' } = {}) {
  const previouslyFocused = opener()

  function focusables(): HTMLElement[] {
    return Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => el.offsetParent !== null || el === document.activeElement,
    )
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Tab') return
    const items = focusables()
    if (items.length === 0) {
      e.preventDefault()
      node.focus()
      return
    }
    const target = wrapTarget(items, document.activeElement, e.shiftKey, node)
    if (target) {
      e.preventDefault()
      target.focus()
    }
  }

  node.addEventListener('keydown', onKeydown)
  queueMicrotask(() => {
    if (options.initial === 'none') return
    const target = focusables().find((el) => !el.hasAttribute('data-autofocus-skip')) ?? node
    target.focus({ preventScroll: true })
  })

  return {
    destroy() {
      node.removeEventListener('keydown', onKeydown)
      // Restore focus only if it is still ours to give back. A dialog that
      // closes with an outro transition is destroyed after another one may
      // already have opened and taken focus; do not pull focus out of it.
      const active = document.activeElement
      const stillOurs = !active || active === document.body || node.contains(active)
      if (stillOurs && previouslyFocused && document.contains(previouslyFocused)) {
        previouslyFocused.focus({ preventScroll: true })
      }
    },
  }
}
