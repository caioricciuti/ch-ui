// Open state of the keyboard shortcut sheet. "?" in the shell and the
// command palette both open it.
let open = $state(false)

export function isShortcutHelpOpen(): boolean {
  return open
}

export function openShortcutHelp(): void {
  open = true
}

export function closeShortcutHelp(): void {
  open = false
}
