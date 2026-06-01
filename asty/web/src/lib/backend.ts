import { origin } from '@asty-web-app/compass'

// apiURL joins the live origin (compass.origin()) with a prefix-
// relative path from routes.ts. Read FRESH on each call so failover
// is observable to subsequent consumers — especially EventSource
// reconnects, which constructor-time captured a stale value before
// the wrapper started rerouting them. When compass is not installed
// the call returns '', yielding a relative URL.
export function apiURL(path: string): string {
  return origin() + path
}
