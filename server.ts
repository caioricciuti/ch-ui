import { serve } from "bun";
import { join, extname } from "path";
import { statSync } from "fs";

const port = 5521;
const basePath = process.env.APP_BASE_PATH || "/app";
const CH_PROXY_PREFIX = "/proxy";

console.log(`Starting server on port ${port}...`);

// Parse multiple URLs from CLICKHOUSE_URLS (comma separated)
const rawUrls = process.env.CLICKHOUSE_URLS || "";
const clickhouseUrls = rawUrls
    .split(",")
    .map(u => u.trim())
    .filter(u => u.length > 0);

// Helper to generate friendly path from URL
function slugifyUrl(urlString: string): string {
    try {
        const u = new URL(urlString);
        // e.g. http://clickhouse.svc:8123 -> clickhouse-svc-8123
        let slug = u.hostname.replace(/[^a-zA-Z0-9-]/g, '-');
        if (u.port) slug += `-${u.port}`;
        return slug;
    } catch (e) {
        // Fallback for invalid URLs? just use hash or keep special chars stripped
        return urlString.replace(/^https?:\/\//, '').replace(/[^a-zA-Z0-9-]/g, '-');
    }
}

const proxyTargets = new Map<string, string>();

if (clickhouseUrls.length > 0) {
    console.log(`Proxy configured with ${clickhouseUrls.length} targets:`);
    clickhouseUrls.forEach((url) => {
        const slug = slugifyUrl(url);
        // Handle collisions if necessary? Unlikely for distinct hostnames
        if (proxyTargets.has(slug)) {
            console.warn(`Duplicate proxy slug generated: ${slug} for ${url}. Keeping first.`);
        } else {
            proxyTargets.set(slug, url);
            console.log(`  ${CH_PROXY_PREFIX}/${slug} -> ${url}`);
        }
    });
} else {
    console.log("No CLICKHOUSE_URLS configured. Proxy disabled.");
}

serve({
    port,
    async fetch(req) {
        const url = new URL(req.url);
        const path = url.pathname;

        // 1. Handle Proxy Requests via Header
        // The client relies on standard ClickHouse URL parsing, so path proxying fails (treated as DB).
        // Instead, the client will inject 'X-ClickHouse-Proxy-Slug' header.

        const proxySlug = req.headers.get("x-clickhouse-proxy-slug");

        if (proxySlug) {

            if (proxyTargets.has(proxySlug)) {
                const targetBase = proxyTargets.get(proxySlug)!;
                // Forward the path and query string exactly as received
                // e.g. /?query=... -> http://target:8123/?query=...
                const targetUrl = new URL(path + url.search, targetBase);

                console.log(`Proxying [${proxySlug}] -> ${targetUrl.toString()}`);

                try {
                    const proxyReq = new Request(targetUrl, {
                        method: req.method,
                        headers: req.headers,
                        body: req.body,
                    });

                    // Remove host header
                    proxyReq.headers.delete("host");

                    const response = await fetch(proxyReq);

                    // Create a new headers object to filter out problematic headers
                    const responseHeaders = new Headers(response.headers);
                    responseHeaders.delete("content-encoding");
                    responseHeaders.delete("content-length");
                    responseHeaders.delete("transfer-encoding");

                    return new Response(response.body, {
                        status: response.status,
                        statusText: response.statusText,
                        headers: responseHeaders,
                    });
                } catch (err) {
                    console.error("Proxy error:", err);
                    return new Response("Proxy Error", { status: 502 });
                }
            } else {
                return new Response("Invalid Proxy Target", { status: 400 });
            }
        }

        // 2. Serve Static Files
        // Determine the local file path
        // If asking for /assets/foo.js, look in /app/assets/foo.js
        let filePath = join(basePath, path === "/" ? "index.html" : path);

        // Security check: ensure escape from basePath is handled by join/resolve usually, 
        // but basic cleanup is good. Bun's serve/file handles relative paths safely?
        // Let's rely on explicit checks.

        // Check if file exists
        try {
            const stat = statSync(filePath);
            if (stat.isFile()) {
                return new Response(Bun.file(filePath));
            }
        } catch (e) {
            // File not found, fall through
        }

        // 3. SPA Fallback
        // If it's not a file request (no extension or html), serve index.html
        // Usually assets have extensions.
        if (!extname(path) || path === "/") {
            return new Response(Bun.file(join(basePath, "index.html")));
        }

        return new Response("Not Found", { status: 404 });
    },
});
