// Resolves the dashboard backend the SPA talks to.
//
// URI parts stay separate: the path prefix lives in routes.ts
// (API_PREFIX); this module owns only the ORIGIN (scheme://host:port).
// apiURL() joins origin + path at the request boundary — the prefix is
// never folded into a "base" string.
//
//   - Same-origin (VITE_ASTY_ORIGIN unset): origin is '' — relative
//     URLs. Use when the SPA is served from the same host as the API.
//   - Cross-origin (VITE_ASTY_ORIGIN set): the SPA (Cloudflare Pages in
//     prod, Vite dev server in dev) calls the cluster directly. The
//     value is a NAME, not an IP — dev `http://asty.test:7060`, prod
//     `https://<cluster-domain>`. That name carries several A-records
//     (the nodes), so node failover is the browser's job at the DNS
//     layer; the SPA stays on one stable origin and the dashboard's
//     CORS layer permits the cross-origin call.
//
// Runtime override (window.__ASTY_ORIGIN__) wins over the build-time env
// so a Cloudflare Pages bundle can be pointed at a cluster without a
// rebuild — same pattern as the auth token (see api/client.ts).
function resolveOrigin(): string {
  if (typeof window !== 'undefined') {
    const injected = (window as { __ASTY_ORIGIN__?: string }).__ASTY_ORIGIN__
    if (injected) return injected
  }
  return (import.meta.env?.VITE_ASTY_ORIGIN as string) ?? ''
}

const ORIGIN = resolveOrigin()

// apiURL joins the configured origin with a prefix-relative path from
// routes.ts. Empty origin yields the original relative URL.
export function apiURL(path: string): string {
  return ORIGIN + path
}
