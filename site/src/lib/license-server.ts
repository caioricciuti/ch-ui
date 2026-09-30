// Base URL of the CH-UI license server (Stripe Checkout, trials, license
// re-send, billing portal). The forms call it from the browser; it allows
// any origin. Override at build time with PUBLIC_LICENSE_SERVER_URL.
export const LICENSE_SERVER_URL: string =
  import.meta.env.PUBLIC_LICENSE_SERVER_URL ?? "https://license.ch-ui.com";

export const SUPPORT_EMAIL = "me@caioricciuti.com";
