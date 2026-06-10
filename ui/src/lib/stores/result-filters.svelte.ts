const STORAGE_KEY = 'ch-ui-result-filters'

const initial = localStorage.getItem(STORAGE_KEY) !== 'false'
let enabled = $state<boolean>(initial)

export function getResultFiltersEnabled(): boolean {
  return enabled
}

export function toggleResultFiltersEnabled(): void {
  enabled = !enabled
  localStorage.setItem(STORAGE_KEY, String(enabled))
}
