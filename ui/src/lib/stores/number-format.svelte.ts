const STORAGE_KEY = 'ch-ui-format-numbers'

const initial = localStorage.getItem(STORAGE_KEY) !== 'false'
let formatNumbers = $state<boolean>(initial)

export function getFormatNumbers(): boolean {
  return formatNumbers
}

export function toggleFormatNumbers(): void {
  formatNumbers = !formatNumbers
  localStorage.setItem(STORAGE_KEY, String(formatNumbers))
}
