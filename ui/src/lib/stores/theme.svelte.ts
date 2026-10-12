export type Theme = 'dark' | 'light'
/** What the user picked. 'system' follows prefers-color-scheme. */
export type ThemePreference = Theme | 'system'

const STORAGE_KEY = 'ch-ui-theme'
const DARK_QUERY = '(prefers-color-scheme: dark)'

function readPreference(): ThemePreference {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'dark' || saved === 'light' || saved === 'system') return saved
  } catch {
    /* storage blocked */
  }
  return 'system'
}

const media = typeof matchMedia === 'function' ? matchMedia(DARK_QUERY) : null

let preference = $state<ThemePreference>(readPreference())
let systemDark = $state(media?.matches ?? true)

media?.addEventListener('change', (e) => {
  systemDark = e.matches
  applyTheme(getTheme())
})

applyTheme(getTheme())

/** The theme in effect: the preference, or the OS setting for 'system'. */
export function getTheme(): Theme {
  if (preference === 'system') return systemDark ? 'dark' : 'light'
  return preference
}

export function getThemePreference(): ThemePreference {
  return preference
}

export function setThemePreference(next: ThemePreference): void {
  preference = next
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    /* storage blocked */
  }
  applyTheme(getTheme())
}

/** Flip to the opposite of what is shown now, as an explicit choice. */
export function toggleTheme(): void {
  setThemePreference(getTheme() === 'dark' ? 'light' : 'dark')
}

/** Cycle system, light, dark. Used by the rail button. */
export function cycleThemePreference(): void {
  const order: ThemePreference[] = ['system', 'light', 'dark']
  setThemePreference(order[(order.indexOf(preference) + 1) % order.length])
}

function applyTheme(t: Theme): void {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('dark', t === 'dark')
}
