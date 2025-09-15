const fs = require("fs");
const path = require("path");

const indexHtmlPath = path.join(__dirname, "index.html");

if (!fs.existsSync(indexHtmlPath)) {
  console.error(`index.html not found at ${indexHtmlPath}`);
  process.exit(1);
}

let indexHtmlContent = fs.readFileSync(indexHtmlPath, "utf8");

// Inject the environment variables
const envVars = {
  VITE_CLICKHOUSE_URL: process.env.VITE_CLICKHOUSE_URL || "",
  VITE_CLICKHOUSE_USER: process.env.VITE_CLICKHOUSE_USER || "",
  VITE_CLICKHOUSE_PASS: process.env.VITE_CLICKHOUSE_PASS || "",
  VITE_CLICKHOUSE_USE_ADVANCED:
    process.env.VITE_CLICKHOUSE_USE_ADVANCED === "true",
  VITE_CLICKHOUSE_CUSTOM_PATH: process.env.VITE_CLICKHOUSE_CUSTOM_PATH || "",
  VITE_CLICKHOUSE_REQUEST_TIMEOUT: parseInt(
    process.env.VITE_CLICKHOUSE_REQUEST_TIMEOUT || "30000",
    10
  ),
  VITE_BASE_PATH: process.env.VITE_BASE_PATH || "/",
};

console.log("Injecting environment variables:");
console.log(
  "VITE_CLICKHOUSE_URL:",
  envVars.VITE_CLICKHOUSE_URL ? "SET" : "NOT SET"
);
console.log(
  "VITE_CLICKHOUSE_USER:",
  envVars.VITE_CLICKHOUSE_USER ? "SET" : "NOT SET"
);
console.log(
  "VITE_CLICKHOUSE_PASS:",
  envVars.VITE_CLICKHOUSE_PASS ? "SET (hidden)" : "NOT SET"
);

const scriptContent = `
<script>
  window.env = ${JSON.stringify(envVars)};
  console.log("Environment variables loaded:", window.env);
</script>
`;

// Check if script already exists (avoid duplicate injection)
if (indexHtmlContent.includes("window.env")) {
  console.log("Environment variables already injected, skipping...");
  process.exit(0);
}

// Update asset paths if base path is provided
const basePath = envVars.VITE_BASE_PATH;
if (basePath && basePath !== "/" && !basePath.startsWith("http")) {
  // Ensure base path starts with / and doesn't end with /
  const normalizedBase = basePath.startsWith("/") ? basePath : `/${basePath}`;
  const baseWithoutTrailing = normalizedBase.endsWith("/")
    ? normalizedBase.slice(0, -1)
    : normalizedBase;

  // Replace asset paths
  indexHtmlContent = indexHtmlContent.replace(
    /href="\/([^"]+)"/g,
    `href="${baseWithoutTrailing}/$1"`
  );
  indexHtmlContent = indexHtmlContent.replace(
    /src="\/([^"]+)"/g,
    `src="${baseWithoutTrailing}/$1"`
  );

  console.log(`Updated asset paths with base path: ${baseWithoutTrailing}`);
}

// Insert the script just before the closing </head> tag
indexHtmlContent = indexHtmlContent.replace(
  "</head>",
  `${scriptContent}</head>`
);

fs.writeFileSync(indexHtmlPath, indexHtmlContent);

console.log("Environment variables injected successfully into", indexHtmlPath);
