const STORAGE_KEY = 'ch-ui-max-result-rows'
const DEFAULT_LIMIT = 1000

const stored = parseInt(localStorage.getItem(STORAGE_KEY) ?? '', 10)
let maxResultRows = $state<number>(isNaN(stored) || stored < 1 ? DEFAULT_LIMIT : stored)

export function getMaxResultRows(): number {
  return maxResultRows
}

export function setMaxResultRows(value: number): void {
  const clamped = Math.max(1, Math.round(value))
  maxResultRows = clamped
  localStorage.setItem(STORAGE_KEY, String(clamped))
}
