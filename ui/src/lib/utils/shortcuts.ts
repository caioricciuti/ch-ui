/**
 * Keyboard shortcut helpers shared by the shell, the tab bar and the
 * shortcut help sheet.
 *
 * Letters are matched on e.key first and on e.code as a fallback. On macOS,
 * Option turns letters into other characters (Option+N is a dead key that
 * reports "Dead" or "˜"), so e.key alone never matches an Option shortcut.
 * e.code ("KeyN") is the physical key and survives Option.
 */
export function isKey(e: Pick<KeyboardEvent, 'key' | 'code'>, letter: string): boolean {
  const l = letter.toLowerCase()
  return e.key.toLowerCase() === l || e.code === `Key${l.toUpperCase()}`
}

/** Focus is in a text field, select or contenteditable (the editor), where plain keys type. */
export function isEditableTarget(target: EventTarget | null): boolean {
  if (typeof HTMLElement === 'undefined' || !(target instanceof HTMLElement)) return false
  return target.tagName === 'INPUT' ||
    target.tagName === 'TEXTAREA' ||
    target.tagName === 'SELECT' ||
    target.isContentEditable
}

export type TabShortcut = 'close' | 'duplicate' | 'split' | 'rename'

/**
 * The tab bar action a key press asks for, or null. Option/Alt+letter and F2
 * are left alone while typing: on macOS Option+S types "ß" and Option+W "∑".
 * The ⌘⇧ forms cannot type anything, so they work everywhere.
 */
export function tabShortcut(e: Pick<KeyboardEvent, 'key' | 'code' | 'altKey' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'target'>): TabShortcut | null {
  const mod = e.metaKey || e.ctrlKey
  const typing = isEditableTarget(e.target)
  const alt = e.altKey && !mod && !typing
  // ⌘⇧W is kept for browsers that allow it; Chrome closes the window.
  if ((alt && isKey(e, 'w')) || (mod && e.shiftKey && isKey(e, 'w'))) return 'close'
  if ((alt && isKey(e, 'd')) || (mod && e.shiftKey && isKey(e, 'd'))) return 'duplicate'
  if (alt && isKey(e, 's')) return 'split'
  if (e.key === 'F2' && !typing) return 'rename'
  return null
}

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

/** Platform labels: ⌘ ⌥ ⇧ on macOS, Ctrl Alt Shift elsewhere. */
export const MOD = isMac ? '⌘' : 'Ctrl'
export const ALT = isMac ? '⌥' : 'Alt'
export const SHIFT = isMac ? '⇧' : 'Shift'

/** One combo as text: "⌥N" on macOS, "Alt+N" elsewhere. */
export function combo(...keys: string[]): string {
  return keys.join(isMac ? '' : '+')
}

export interface ShortcutEntry {
  keys: string[]
  label: string
}

export interface ShortcutGroup {
  title: string
  items: ShortcutEntry[]
}

/**
 * Everything the app binds, for the "?" help sheet. Combos the browser
 * reserves are not offered: Chrome keeps ⌘⇧N / Ctrl+Shift+N (incognito) and
 * ⌘⇧W / Ctrl+Shift+W (close window) for itself, so new query and close tab
 * use Option/Alt instead.
 */
export function shortcutGroups(): ShortcutGroup[] {
  return [
    {
      title: 'Anywhere',
      items: [
        { keys: [MOD, 'K'], label: 'Open or close the command palette' },
        { keys: ['/'], label: 'Open the command palette' },
        { keys: [ALT, 'K'], label: 'Open the command palette' },
        { keys: ['?'], label: 'Show keyboard shortcuts' },
        { keys: [ALT, 'N'], label: 'New query tab' },
        { keys: [MOD, 'B'], label: 'Show or hide the schema explorer' },
      ],
    },
    {
      title: 'Tabs',
      items: [
        { keys: [ALT, 'W'], label: 'Close the current tab' },
        { keys: [ALT, 'D'], label: 'Duplicate the current query tab' },
        { keys: [ALT, 'S'], label: 'Open the current tab in a split' },
        { keys: ['F2'], label: 'Rename the current tab' },
      ],
    },
    {
      title: 'SQL editor',
      items: [
        { keys: [MOD, 'Enter'], label: 'Run the statement at the cursor, or the selection' },
        { keys: [MOD, 'S'], label: 'Save the query' },
        { keys: [SHIFT, ALT, 'F'], label: 'Format SQL' },
        { keys: ['Esc'], label: 'Cancel the running query' },
        { keys: [MOD, '.'], label: 'Cancel the running query' },
        { keys: ['Tab'], label: 'Indent (Esc, then Tab, moves focus out)' },
      ],
    },
  ]
}
