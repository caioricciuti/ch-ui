// Writes the docs changelog page from the repository's CHANGELOG.md.
//
// CHANGELOG.md stays where it is, written for GitHub; this script turns it
// into src/content/docs/docs/changelog.md with the frontmatter Starlight
// needs. The copy is generated and gitignored: edit CHANGELOG.md. Runs under
// bun before every dev and build, with nothing but the standard library.
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { readReleases } from "../src/lib/changelog";

const out = resolve(process.cwd(), "src/content/docs/docs/changelog.md");
const releases = readReleases();

const page = [
  "---",
  "title: Changelog",
  'description: "Every CH-UI release: what changed, what was fixed, and what to check before upgrading."',
  // One entry per release in the table of contents, not one per "Fixed".
  "tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 2 }",
  "---",
  "",
  "Every release is also on [GitHub Releases](https://github.com/caioricciuti/ch-ui/releases) with signed checksums",
  "and an SBOM. Subscribe to the [RSS feed](/changelog.xml) to hear about new ones.",
  "",
  ...releases.flatMap((r) => [`## v${r.version} - ${r.date}`, "", r.body, ""]),
].join("\n");

mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, page);
console.log(`CHANGELOG.md -> docs/changelog.md (${releases.length} releases, newest v${releases[0].version})`);
