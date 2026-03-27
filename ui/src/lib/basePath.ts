/**
 * Base path utility for subpath deployments.
 *
 * Restores the basePath.ts that existed in v1.6.x (removed in v2.0.0 rewrite).
 * Checks runtime window.env first (for Docker inject-env), falls back to
 * build-time import.meta.env.BASE_URL (set via VITE_BASE_PATH).
 */

declare global {
  interface Window {
    env?: { VITE_BASE_PATH?: string }
  }
}

/** Returns the base path without trailing slash (empty string for root). */
export function getBasePath(): string {
  if (typeof window !== 'undefined' && window.env?.VITE_BASE_PATH) {
    return window.env.VITE_BASE_PATH.replace(/\/$/, '')
  }
  const base = import.meta.env.BASE_URL ?? '/'
  return base.endsWith('/') ? base.slice(0, -1) : base
}

/** Prepends the base path to an absolute path (path should start with /). */
export const withBase = (path: string) => {
  const base = getBasePath()
  return base ? base + path : path
}

/** Strips the base path prefix from a pathname. */
export const stripBase = (path: string) => {
  const base = getBasePath()
  if (!base) return path
  return path.startsWith(base) ? (path.slice(base.length) || '/') : path
}
