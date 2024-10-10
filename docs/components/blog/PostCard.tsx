// components/blog/PostCard.tsx
import Link from "next/link";
import Image from "next/image";

export function PostCard({ post }) {
  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg overflow-hidden shadow-lg">
      <Image
        src={post.frontMatter.image || "/default-post-image.jpg"}
        alt={post.frontMatter.title}
        width={600}
        height={300}
        className="object-cover w-full h-48"
      />
      <div className="p-6">
        <Link href={post.route}>
          <h3 className="text-xl font-semibold mb-2 hover:underline">
            {post.frontMatter.title}
          </h3>
        </Link>
        <p className="text-gray-600 dark:text-gray-300 mb-4">
          {post.frontMatter.description}
        </p>
        <div className="flex justify-between items-center">
          <Link href={`/blog/authors/${post.frontMatter.author}`}>
            <span className="text-sm text-blue-500 hover:underline">
              {post.frontMatter.author}
            </span>
          </Link>
          <span className="text-sm text-gray-500">{post.frontMatter.date}</span>
        </div>
      </div>
    </div>
  );
}
