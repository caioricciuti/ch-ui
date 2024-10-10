// pages/blog/_meta.ts
export default {
  index: {
    title: "Blog",
    type: "page",
  },
  posts: {
    title: "All Posts",
    type: "page",
  },
  tags: {
    title: "Tags",
    type: "page",
  },
  authors: {
    title: "Authors",
    type: "page",
  },
  "*": {
    type: "page",
    theme: {
      typesetting: "article",
    },
  },
};
