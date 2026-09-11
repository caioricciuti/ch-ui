/**
 * Navigation chrome state shared between the rail, the context panel and
 * the pages: the current `?section=` and whether the panel is open.
 */

let section = $state<string | null>(null)

export function getSection(): string | null {
  return section
}

/** Re-read `?section=` from the URL. Called by the router on every route change. */
export function syncSectionFromUrl(): void {
  if (typeof window === 'undefined') return
  section = new URLSearchParams(window.location.search).get('section')
}

/** Switch section in place. replaceState, so Back leaves the page, not the section. */
export function setSection(id: string): void {
  if (typeof window === 'undefined') return
  const url = new URL(window.location.href)
  if (url.searchParams.get('section') !== id) {
    url.searchParams.set('section', id)
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }
  section = id
}

// Collapse is remembered per rail group: hiding the schema tree on the
// workspace must not hide Governance's section list.
const PANEL_KEY = 'ch-ui-panel-collapsed'

function loadCollapsed(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(PANEL_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : {}
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, boolean>) : {}
  } catch {
    return {}
  }
}

let panelCollapsed = $state<Record<string, boolean>>(loadCollapsed())

export function isPanelCollapsed(group: string): boolean {
  return panelCollapsed[group] === true
}

export function setPanelCollapsed(group: string, value: boolean): void {
  panelCollapsed = { ...panelCollapsed, [group]: value }
  try {
    localStorage.setItem(PANEL_KEY, JSON.stringify(panelCollapsed))
  } catch {
    /* private mode */
  }
}

export function togglePanel(group: string): void {
  setPanelCollapsed(group, !isPanelCollapsed(group))
}
