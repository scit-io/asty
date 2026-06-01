// @asty-web-app/compass — locality-aware origin selection for Asty SPAs.
//
// Bootstrap once:
//
//   import { install } from '@asty-web-app/compass'
//   await install({ bootstrapUrl: '/api/v1', fallbackOrigin: '/' })
//
// Then import the package's `fetch` and `EventSource` and use them
// like the native globals — they handle node failover internally:
//
//   import { fetch, EventSource } from '@asty-web-app/compass'
//   const res = await fetch('/api/v1/services')
//   const es = new EventSource('/dashboard/v1/stream')
//
// `origin()` returns the live preferred backend; `subscribe()`
// observes failover events. No window globals are touched.

export {
  install,
  origin,
  subscribe,
  fetch,
  EventSource,
} from './install'

export { select, selectOrigin } from './select'
export { createApiFetch } from './apiFetch'
export type {
  SelectOriginOptions,
  SelectionResult,
  Candidate,
  CompassFetchOptions,
  InstallOptions,
  CompassClient,
} from './types'
