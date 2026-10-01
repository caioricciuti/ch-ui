// /changelog.xml: the releases as an RSS 2.0 feed, one item per release,
// newest first, built from CHANGELOG.md.
import type { APIRoute } from "astro";
import { plainText, readReleases } from "../lib/changelog";

function xmlEscape(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

export const GET: APIRoute = ({ site }) => {
  const page = new URL("/docs/changelog/", site).href;
  const items = readReleases().map((r) => {
    // The page anchors each release by its heading, "v2.14.1 - 2026-09-29".
    const anchor = `v${r.version.replaceAll(".", "")}---${r.date}`;
    return [
      "    <item>",
      `      <title>CH-UI v${r.version}</title>`,
      `      <link>${page}#${anchor}</link>`,
      `      <guid isPermaLink="false">ch-ui-v${r.version}</guid>`,
      `      <pubDate>${new Date(`${r.date}T12:00:00Z`).toUTCString()}</pubDate>`,
      `      <description>${xmlEscape(plainText(r.body))}</description>`,
      "    </item>",
    ].join("\n");
  });
  const feed = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<rss version="2.0">',
    "  <channel>",
    "    <title>CH-UI Changelog</title>",
    `    <link>${page}</link>`,
    "    <description>Release notes for CH-UI, the self-hosted ClickHouse workspace.</description>",
    "    <language>en</language>",
    ...items,
    "  </channel>",
    "</rss>",
    "",
  ].join("\n");
  return new Response(feed, { headers: { "Content-Type": "application/rss+xml; charset=utf-8" } });
};
