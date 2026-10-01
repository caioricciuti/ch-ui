// /llms.txt: the docs as a plain list for language models, in the sidebar's
// groups and order, with each page's own title and description.
import type { APIRoute } from "astro";
import { getCollection } from "astro:content";
import { sidebar } from "../sidebar.mjs";

const INTRO =
  "CH-UI is the open-source control plane for ClickHouse: SQL editor, dashboards, pipelines, query analytics, cluster monitoring, governance, and an AI copilot, self-hosted as a single binary. The core is Apache-2.0; Pro features unlock with a flat $1,199/year per-instance license activated offline.";

export const GET: APIRoute = async ({ site }) => {
  const pages = new Map((await getCollection("docs")).map((entry) => [entry.id, entry.data]));
  const lines = ["# CH-UI", "", `> ${INTRO}`, ""];

  for (const group of sidebar) {
    lines.push(`## ${group.label}`, "");
    for (const item of group.items) {
      const slug = typeof item === "string" ? item : item.slug;
      const page = pages.get(slug);
      if (!page) throw new Error(`src/sidebar.mjs names "${slug}", which is not a docs page`);
      const url = new URL(`/${slug}/`, site).href;
      lines.push(`- [${page.title}](${url})${page.description ? `: ${page.description}` : ""}`);
    }
    lines.push("");
  }

  return new Response(`${lines.join("\n").trimEnd()}\n`, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
