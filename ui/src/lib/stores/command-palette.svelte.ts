let open = $state(false)

export function isCommandPaletteOpen(): boolean {
  return open
}

export function openCommandPalette(): void {
  open = true
}

export function closeCommandPalette(): void {
  open = false
}

export function toggleCommandPalette(): void {
  open = !open
}
