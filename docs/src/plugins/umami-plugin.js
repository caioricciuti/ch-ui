module.exports = function (context, options) {
  return {
    name: "umami-plugin",
    injectHtmlTags() {
      return {
        headTags: [
          {
            tagName: "script",
            attributes: {
              defer: true,
              "data-website-id": "124b0227-b529-4a81-83f5-b28fdcc7a7d8",
              src: "http://localhost:3000/script.js",
            },
          },
        ],
      };
    },
  };
};
