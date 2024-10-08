export default {
  index: {
    type: "page",
    title: "CH-UI",
    display: "hidden",
    theme: {
      layout: "full",
      timestamp: false,
    },
  },
  docs: {
    type: "page",
    title: "Documentation",
  },
  blog: {
    type: "page",
    title: "Blog",
    theme: {
      typesetting: "article",
      breadcrumb: true,
      footer: true,
      sidebar: false,
      toc: true,
      pagination: true,
    },
  },
  support: {
    type: "page",
    title: "Support",
    href: "https://www.buymeacoffee.com/caioricciuti?utm_source=ch-ui-docs&utm_medium=header",
    newWindow: true,
  },
  404: {
    type: "page",
    theme: {
      timestamp: false,
      typesetting: "article",
    },
  },
};
