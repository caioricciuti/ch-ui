const fs = require("fs");
const path = require("path");

const indexHtmlPath = path.join(__dirname, "index.html");
let indexHtmlContent = fs.readFileSync(indexHtmlPath, "utf8");

// Inject the environment variables
const envVars = {
  VITE_CLICKHOUSE_URL: process.env.VITE_CLICKHOUSE_URL || "",
  VITE_CLICKHOUSE_USER: process.env.VITE_CLICKHOUSE_USER || "",
  VITE_CLICKHOUSE_PASS: process.env.VITE_CLICKHOUSE_PASS || "",
  VITE_CLICKHOUSE_USE_ADVANCED: process.env.VITE_CLICKHOUSE_USE_ADVANCED || "",
  VITE_CLICKHOUSE_CUSTOM_PATH: process.env.VITE_CLICKHOUSE_CUSTOM_PATH || "",
};

const scriptContent = `
<script>
  window.env = ${JSON.stringify(envVars)};
</script>
`;

// Insert the script just before the closing </head> tag
indexHtmlContent = indexHtmlContent.replace(
  "</head>",
  `${scriptContent}</head>`
);

fs.writeFileSync(indexHtmlPath, indexHtmlContent);

console.log("Environment variables injected successfully");
