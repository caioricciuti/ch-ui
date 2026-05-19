const CACHE_KEY = 'chui_latest_version'
const CACHE_TTL = 6 * 60 * 60 * 1000 // 6 hours

let latestVersion = $state<string | null>(null)

interface CachedVersion {
  version: string
  checkedAt: number
}

function parseVersion(v: string): number[] {
  return v.replace(/^v/, '').split('.').map(Number)
}

function isNewer(latest: string, current: string): boolean {
  const l = parseVersion(latest)
  const c = parseVersion(current)
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0
    const cv = c[i] ?? 0
    if (lv > cv) return true
    if (lv < cv) return false
  }
  return false
}

export async function checkForUpdate(currentVersion: string): Promise<void> {
  try {
    const cached = sessionStorage.getItem(CACHE_KEY)
    if (cached) {
      const parsed: CachedVersion = JSON.parse(cached)
      if (Date.now() - parsed.checkedAt < CACHE_TTL) {
        latestVersion = parsed.version
        return
      }
    }

    const res = await fetch('https://api.github.com/repos/caioricciuti/ch-ui/releases/latest', {
      headers: { Accept: 'application/vnd.github.v3+json' },
    })
    if (!res.ok) return

    const data = await res.json()
    const tag: string = data.tag_name ?? ''
    if (!tag) return

    latestVersion = tag
    sessionStorage.setItem(CACHE_KEY, JSON.stringify({ version: tag, checkedAt: Date.now() }))
  } catch {
    // silent fail — update check is best-effort
  }
}

export function getLatestVersion(): string | null {
  return latestVersion
}

export function hasUpdate(currentVersion: string): boolean {
  if (!latestVersion) return false
  return isNewer(latestVersion, currentVersion)
}
