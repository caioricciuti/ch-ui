import { getTheme } from '../../stores/theme.svelte'

type Theme = 'dark' | 'light'

/**
 * A theme forced for this page only (a public dashboard opened with
 * ?theme=light), without touching the viewer's saved preference.
 */
let override = $state<Theme | null>(null)

export function setDashboardThemeOverride(theme: Theme | null): void {
  override = theme
  const t = theme ?? getTheme()
  document.documentElement.classList.toggle('dark', t === 'dark')
}

/** Whether dashboard panels should draw for a dark background. */
export function dashboardIsDark(): boolean {
  return (override ?? getTheme()) === 'dark'
}
