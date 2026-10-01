// The only server-side code on ch-ui.com: POST /api/newsletter adds an
// address to the Resend list. Everything else is a static asset, and only
// /api/* reaches this script (assets.run_worker_first in wrangler.jsonc).
//
// RESEND_API_KEY (full access, not sending-only) and RESEND_SEGMENT_ID are
// Worker secrets, set in the Cloudflare dashboard. They are never in this
// repository and never reach the browser.

// The bindings this Worker uses, declared here so the site needs no types
// package for three methods.
interface Env {
  RESEND_API_KEY?: string;
  RESEND_SEGMENT_ID?: string;
  NEWSLETTER_LIMITER: { limit(options: { key: string }): Promise<{ success: boolean }> };
  ASSETS: { fetch(request: Request): Promise<Response> };
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function json(body: Record<string, unknown>, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });
}

// Cloudflare sets CF-Connecting-IP itself; a client cannot forge it. IPv6
// clients usually hold a whole /64, so bucket them by it; otherwise one host
// could rotate through fresh addresses and never hit the limit.
function rateKey(request: Request): string {
  const ip = request.headers.get("CF-Connecting-IP") ?? "unknown";
  if (!ip.includes(":")) return ip;
  const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(ip);
  if (mapped) return mapped[1];
  const [head, tail = ""] = ip.toLowerCase().split("::");
  const left = head ? head.split(":") : [];
  const right = tail ? tail.split(":") : [];
  const groups = ip.includes("::")
    ? [...left, ...Array<string>(Math.max(0, 8 - left.length - right.length)).fill("0"), ...right]
    : left;
  return `${groups.slice(0, 4).map((g) => g.replace(/^0+(?=.)/, "")).join(":")}::/64`;
}

async function subscribe(request: Request, env: Env): Promise<Response> {
  if (request.method !== "POST") {
    return json({ error: "Method not allowed" }, 405);
  }

  const { success } = await env.NEWSLETTER_LIMITER.limit({ key: rateKey(request) });
  if (!success) {
    return json({ error: "Too many requests. Please wait a minute and try again." }, 429);
  }

  let email = "";
  try {
    const body = (await request.json()) as { email?: unknown };
    if (typeof body.email === "string") email = body.email.trim();
  } catch {
    // fall through to the validation error
  }
  if (!email || email.length > 320 || !EMAIL_RE.test(email)) {
    return json({ error: "Please enter a valid email address." }, 400);
  }

  if (!env.RESEND_API_KEY || !env.RESEND_SEGMENT_ID) {
    return json({ error: "Newsletter signup is not available right now. Please try again later." }, 503);
  }

  try {
    const res = await fetch("https://api.resend.com/contacts", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${env.RESEND_API_KEY}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ email, unsubscribed: false, segments: [{ id: env.RESEND_SEGMENT_ID }] }),
    });
    if (!res.ok) {
      // The status only: Resend's body can echo the address.
      console.error("Resend contacts error", res.status);
      return json({ error: "Something went wrong. Please try again later." }, 502);
    }
  } catch {
    console.error("Resend request failed");
    return json({ error: "Something went wrong. Please try again later." }, 502);
  }

  return json({ success: true }, 200);
}

// Workers load the handler from the module's default export.
export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const { pathname } = new URL(request.url);
    if (pathname === "/api/newsletter") return subscribe(request, env);
    return env.ASSETS.fetch(request);
  },
};
