// components/get-all-tags.ts
import { getPagesUnderRoute } from "nextra/context";

export function getAllTags(route: string) {
  const pages = getPagesUnderRoute(route);
  const tags: Record<string, number> = {};

  pages.forEach((page) => {
    if (page.frontMatter && page.frontMatter.tags) {
      page.frontMatter.tags.forEach((tag: string) => {
        if (tags[tag]) {
          tags[tag]++;
        } else {
          tags[tag] = 1;
        }
      });
    }
  });

  return tags;
}
