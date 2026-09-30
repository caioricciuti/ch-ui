// The released version, read at build time from the repository's VERSION
// file, so the site never names a version the repo is not at. The build
// runs with site/ as its working directory, locally and on Cloudflare.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

function readVersion(): string | null {
  try {
    const raw = readFileSync(resolve(process.cwd(), "../VERSION"), "utf-8").trim();
    return /^v\d+\.\d+\.\d+$/.test(raw) ? raw : null;
  } catch {
    return null;
  }
}

/** For example "v2.14.1", or null when the file is missing or malformed. */
export const VERSION: string | null = readVersion();
