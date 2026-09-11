/**
 * Svelte action for dialogs: keeps Tab inside the node, focuses the first
 * focusable element (or the node) on mount, and returns focus to whatever
 * had it when the dialog closes.
 */
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function trapFocus(node: HTMLElement, options: { initial?: 'first' | 'none' } = {}) {
  const previouslyFocused = document.activeElement as HTMLElement | null

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
    const first = items[0]
    const last = items[items.length - 1]
    const active = document.activeElement
    if (e.shiftKey && (active === first || !node.contains(active))) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && (active === last || !node.contains(active))) {
      e.preventDefault()
      first.focus()
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
      if (previouslyFocused && document.contains(previouslyFocused)) {
        previouslyFocused.focus({ preventScroll: true })
      }
    },
  }
}
