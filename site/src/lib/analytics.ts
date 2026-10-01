// Wrapper around the self-hosted Umami tracker (loaded in the page head).
// Analytics must never throw or break the page.

type AnalyticsProps = Record<string, string | number | boolean>;

interface UmamiAPI {
  track: (name: string, data?: AnalyticsProps) => void;
}

declare global {
  interface Window {
    umami?: UmamiAPI;
  }
}

/** Record a custom event. A no-op before the tracker script has loaded. */
export function track(event: string, props?: AnalyticsProps): void {
  try {
    window.umami?.track(event, props);
  } catch {
    /* never let analytics break the page */
  }
}
