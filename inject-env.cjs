const fs = require("fs");
const path = require("path");

const indexHtmlPath = path.join(__dirname, "index.html");

if (!fs.existsSync(indexHtmlPath)) {
  console.error(`index.html not found at ${indexHtmlPath}`);
  process.exit(1);
}

let indexHtmlContent = fs.readFileSync(indexHtmlPath, "utf8");

// Inject the environment variables
const clickhouseUrls = (process.env.CLICKHOUSE_URLS || "")
  .split(",")
  .map(u => u.trim())
  .filter(u => u.length > 0);

// Helper to generate friendly path from URL (Must match server.ts logic)
function slugifyUrl(urlString) {
  try {
    // Basic node URL parsing
    const u = new URL(urlString);
    let slug = u.hostname.replace(/[^a-zA-Z0-9-]/g, '-');
    if (u.port) slug += `-${u.port}`;
    return slug;
  } catch (e) {
    return urlString.replace(/^https?:\/\//, '').replace(/[^a-zA-Z0-9-]/g, '-');
  }
}

const envVars = {
  VITE_CLICKHOUSE_URLS: clickhouseUrls.length > 0
    ? clickhouseUrls.map(u => `/proxy/${slugifyUrl(u)}`)
    : (process.env.VITE_CLICKHOUSE_URLS || "")
      .split(",")
      .filter((url) => url.trim() !== ""),
};

console.log("Injecting environment variables:");
console.log(
  "VITE_CLICKHOUSE_URLS:",
  envVars.VITE_CLICKHOUSE_URLS && envVars.VITE_CLICKHOUSE_URLS.length > 0
    ? `SET (${envVars.VITE_CLICKHOUSE_URLS.length} urls)`
    : "NOT SET"
);


const scriptContent = `
<script>
  window.env = ${JSON.stringify(envVars)};
</script>
`;

// Check if script already exists (avoid duplicate injection)
if (indexHtmlContent.includes("window.env")) {
  console.log("Environment variables already injected, skipping...");
  process.exit(0);
}



// Insert the script just before the closing </head> tag
indexHtmlContent = indexHtmlContent.replace(
  "</head>",
  `${scriptContent}</head>`
);

fs.writeFileSync(indexHtmlPath, indexHtmlContent);

console.log("Environment variables injected successfully into", indexHtmlPath);
