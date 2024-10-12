"use client";

import Link from "next/link";
import ColourBlogHeader from "./ColourBlogHeader";

interface Post {
  frontMatter: {
    image?: string;
    title: string;
    description: string;
    author: {
      name: string;
      github: string;
    };
    date: string;
  };
  route: string;
}

export function PostCard({ post }: { post: Post }) {
  return (
    <div className="rounded-lg shadow-md h-64 overflow-hidden dark:border dark:border-gray-600">
      <div className="w-full rounded no-underline hover:no-underline">
        <Link href={post.route}>
          <ColourBlogHeader
            title={post.frontMatter.title}
            date={new Date(post.frontMatter.date).toLocaleDateString("en-US", {
              year: "numeric",
              month: "long",
              day: "numeric",
            })}
          />
        </Link>
      </div>
      <div className="p-6">
        <h3 className="text-lg font-semibold mb-2 line-clamp-2 overflow-hidden">
          {post.frontMatter.description}
        </h3>

        {post.frontMatter.author.name && post.frontMatter.author.github && (
          <div className="flex justify-between items-center">
            <Link target="_blank" href={`${post.frontMatter.author.github}`}>
              <span className="text-sm text-blue-500 hover:underline">
                {post.frontMatter.author.name || "Unknown author"}
              </span>
            </Link>
          </div>
        )}
      </div>
    </div>
  );
}
