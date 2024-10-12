// pages/blog/_meta.ts
export default {
  index: {
    title: "Blog",
    type: "page",
  },
  posts: {
    title: "Posts",
    type: "hidden",
  },
  "*": {
    type: "page",
    theme: {
      typesetting: "article",
    },
  },
};
