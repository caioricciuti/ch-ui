"use client";
// components/blog/PostCard.tsx
import Link from "next/link";
import Image from "next/image";

interface Post {
  frontMatter: {
    image?: string;
    title: string;
    description: string;
    author: string;
    date: string;
  };
  route: string;
}

export function PostCard({ post }: { post: Post }) {
  return (
    <div className=" border-orange-500 border-2 p-2 rounded-lg overflow-hidden shadow-lg">
      {post.frontMatter.image ? (
        <Image
          src={post.frontMatter.image}
          alt={post.frontMatter.title}
          width={600}
          height={300}
          className="object-cover w-full h-48"
        />
      ) : (
        <div className="w-full h-48 rounded">
          <div className="w-full h-full bg-gray-300 dark:bg-gray-800"></div>
        </div>
      )}
      <div className="p-6">
        <Link href={post.route}>
          <h3 className="text-xl font-semibold mb-2 hover:underline">
            {post.frontMatter.title}
          </h3>
        </Link>
        <p className="text-gray-600 dark:text-gray-300 mb-4">
          {post.frontMatter.description}
        </p>
        <div className="text-sm text-gray-500 dark:text-gray-400">
          {/* //show only day/month/year from post.frontMatter.date (found: [object Date]) to utc */}
          {new Date(post.frontMatter.date).toLocaleDateString("en-US", {
            year: "numeric",
            month: "long",
            day: "numeric",
          })}
        </div>
        {post.frontMatter.author.name && post.frontMatter.author.github && (
          <div className="flex justify-between items-center">
            <Link target="_blank" href={`${post.frontMatter.author.github}`}>
              <span className="text-sm text-blue-500 hover:underline">
                {post.frontMatter.author.name || "Unknown author"}
              </span>
            </Link>
            {}
          </div>
        )}
      </div>
    </div>
  );
}
