// Open state of the connection switcher, so the sidebar button and the
// command palette open the same popover.
let open = $state(false)

export function isConnectionSwitcherOpen(): boolean {
  return open
}

export function openConnectionSwitcher(): void {
  open = true
}

export function closeConnectionSwitcher(): void {
  open = false
}
