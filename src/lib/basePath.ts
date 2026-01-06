/**
 * Gets the base path for the application.
 * Checks runtime window.env first (for Docker), falls back to build-time BASE_URL.
 */
export function getBasePath(): string {
  return import.meta.env.BASE_URL;
}

/**
 * Constructs a URL with the correct base path.
 * @param path - The path to append (should not start with /)
 */
export function withBasePath(path: string): string {
  const base = getBasePath();
  const cleanPath = path.startsWith("/") ? path.slice(1) : path;
  return `${base}${cleanPath}`;
}
