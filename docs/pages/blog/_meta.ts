// pages/blog/_meta.ts
export default {
  index: {
    title: "Blog",
    type: "page",
    display: "hidden",
    theme: {
      layout: "full",
    },
  },
  posts: {
    title: "All Posts",
    type: "page",
  },
  tags: {
    title: "Tags",
    type: "page",
  },
  "*": {
    // This will apply to all other blog posts
    type: "page",
    theme: {
      typesetting: "article",
    },
  },
};
