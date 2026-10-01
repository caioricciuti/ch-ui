// The repository's CHANGELOG.md, parsed. It is the only changelog: the docs
// page (scripts/sync-changelog.ts) and the RSS feed (pages/changelog.xml.ts)
// are both built from it, so neither can fall behind a release. The build
// runs with site/ as its working directory, locally and on Cloudflare.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const GITHUB_BLOB = "https://github.com/caioricciuti/ch-ui/blob/main/";

export interface Release {
  /** "2.14.1" */
  version: string;
  /** "2026-09-29" */
  date: string;
  /** The release's Markdown, without its heading. */
  body: string;
}

// Links written for GitHub (a bare file path) point at the file on GitHub;
// absolute URLs, site paths and anchors stay as they are.
function rewriteLinks(markdown: string): string {
  return markdown.replace(/\]\(([^)\s]+)\)/g, (whole, target: string) =>
    /^[a-z]+:/i.test(target) || target.startsWith("#") || target.startsWith("/")
      ? whole
      : `](${GITHUB_BLOB}${target})`,
  );
}

/** Every released version, newest first. "Unreleased" is left out. */
export function readReleases(): Release[] {
  const raw = readFileSync(resolve(process.cwd(), "../CHANGELOG.md"), "utf-8");
  // The reference-style link definitions at the foot belong to the bracketed
  // headings, which are rewritten below.
  const text = raw.replace(/^\[[^\]]+\]:\s+\S+\s*$/gm, "");
  const heading = /^## \[(\d+\.\d+\.\d+)\] - (\d{4}-\d{2}-\d{2})\s*$/gm;
  const marks = [...text.matchAll(heading)];
  if (marks.length === 0) {
    throw new Error('No "## [X.Y.Z] - YYYY-MM-DD" headings found in CHANGELOG.md');
  }
  return marks.map((mark, i) => {
    const start = mark.index + mark[0].length;
    const end = i + 1 < marks.length ? marks[i + 1].index : text.length;
    return { version: mark[1], date: mark[2], body: rewriteLinks(text.slice(start, end).trim()) };
  });
}

/** A release body as one line of plain text, for a feed description. */
export function plainText(markdown: string, max = 400): string {
  const text = markdown
    .replace(/^#+\s+/gm, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[*_`]/g, "")
    .replace(/^\s*[-*]\s+/gm, "")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > max ? `${text.slice(0, max - 3).trimEnd()}...` : text;
}
