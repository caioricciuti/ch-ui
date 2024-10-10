// components/blog/TagList.tsx
import Link from "next/link";
import { getAllTags } from "./GetAllTags";

export function TagList() {
  const tags = getAllTags("/blog/posts");
  return (
    <div className="flex flex-wrap gap-2">
      {Object.entries(tags).map(([tag, count]) => (
        <Link
          key={tag}
          href={`/blog/tags/${tag}`}
          className="px-3 py-1 bg-gray-200 dark:bg-gray-700 rounded-full text-sm hover:bg-gray-300 dark:hover:bg-gray-600"
        >
          {tag} ({count})
        </Link>
      ))}
    </div>
  );
}