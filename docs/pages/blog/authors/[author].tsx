import Link from "next/link";
import Image from "next/image";
import { getPagesUnderRoute } from "nextra/context";

const authors = {
  "caio-ricciuti": {
    name: "Caio Ricciuti",
    bio: "Creator of CH-UI and passionate about data visualization.",
    image: "https://avatars.githubusercontent.com/u/1024025?v=4",
  }
  // Add more authors here
};

export default function AuthorsPage() {
  let posts = [];
  try {
    posts = getPagesUnderRoute("/blog/posts") || [];
  } catch (error) {
    console.error("Error fetching blog posts:", error);
  }

  const authorPosts = Object.keys(authors).reduce((acc, author) => {
    acc[author] = posts.filter(post => post.frontMatter?.author === author);
    return acc;
  }, {});

  return (
    <div className="max-w-5xl mx-auto px-4">
      <h1 className="text-4xl font-bold mb-8">Authors</h1>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
        {Object.entries(authors).map(([slug, author]) => (
          <div key={slug} className="bg-white dark:bg-gray-800 rounded-lg overflow-hidden shadow-lg p-6">
            <div className="flex items-center mb-4">
              <Image
                src={author.image}
                alt={author.name}
                width={64}
                height={64}
                className="rounded-full mr-4"
              />
              <div>
                <h2 className="text-xl font-semibold">{author.name}</h2>
                <p className="text-gray-600 dark:text-gray-300">{author.bio}</p>
              </div>
            </div>
            <h3 className="text-lg font-semibold mb-2">Recent Posts</h3>
            {authorPosts[slug] && authorPosts[slug].length > 0 ? (
              <ul className="space-y-2">
                {authorPosts[slug].slice(0, 3).map(post => (
                  <li key={post.route}>
                    <Link href={post.route} className="hover:underline">
                      {post.frontMatter.title}
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p>No posts found for this author.</p>
            )}
            <Link href={`/blog/authors/${slug}`} className="text-blue-500 hover:underline mt-4 inline-block">
              View all posts
            </Link>
          </div>
        ))}
      </div>
    </div>
  );
}