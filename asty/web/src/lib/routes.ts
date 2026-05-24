// Single source of truth for every URL the SPA produces, both for
// react-router navigation and for backend HTTP/SSE requests. NAV
// holds the segment per resource (defined once); `routes` re-exports
// it for `<Link to=>` / `navigate(...)`; `apiPaths` mirrors NAV under
// API_PREFIX and adds the write/metadata endpoints that have no SPA
// route of their own (scale, drain, kill, restart, …).

// API_PREFIX — backend mount point. The orchestrator's default; can
// be overridden per deployment via A_DASHBOARD_PREFIX. Mirror this
// in vite.config.ts's dev-server proxy when changing.
export const API_PREFIX = '/dashboard/v1'

const NAV = {
  cluster:            '/',
  clusterLogs:        '/logs',
  nodes:              '/nodes',
  services:           '/services',
  node:               (id: string) => `/nodes/${id}`,
  nodeLogs:           (id: string) => `/nodes/${id}/logs`,
  nodeAllocations:    (id: string) => `/nodes/${id}/allocations`,
  allocation:         (nodeId: string, allocId: string) =>
    `/nodes/${nodeId}/allocations/${allocId}`,
  allocationLogs:     (nodeId: string, allocId: string) =>
    `/nodes/${nodeId}/allocations/${allocId}/logs`,
  service:            (name: string) => `/services/${name}`,
  serviceAllocations: (name: string) => `/services/${name}/allocations`,
  serviceAutoscaler:  (name: string) => `/services/${name}/autoscaler`,
  serviceDeploy:      (name: string) => `/services/${name}/deploy`,
} as const

export const routes = NAV

// Local prefixers: `p` for string entries, `pf` for builder functions.
// Short names keep the apiPaths table readable.
const p = (s: string) => API_PREFIX + s
const pf = <A extends unknown[]>(f: (...a: A) => string) =>
  (...a: A) => API_PREFIX + f(...a)

export const apiPaths = {
  // Navigable resources — share segments with `routes`.
  cluster:            p(NAV.cluster),
  clusterLogs:        p(NAV.clusterLogs),
  nodes:              p(NAV.nodes),
  services:           p(NAV.services),
  node:               pf(NAV.node),
  nodeLogs:           pf(NAV.nodeLogs),
  service:            pf(NAV.service),
  allocation:         pf(NAV.allocation),
  allocationLogs:     pf(NAV.allocationLogs),
  serviceAutoscaler:  pf(NAV.serviceAutoscaler),
  serviceDeploy:      pf(NAV.serviceDeploy),
  // Write / metadata endpoints — no SPA route mirror.
  serviceVersions:    (name: string) => `${API_PREFIX}/services/${name}/versions`,
  serviceScale:       (name: string) => `${API_PREFIX}/services/${name}/scale`,
  nodeDrain:          (id: string) => `${API_PREFIX}/nodes/${id}/drain`,
  nodePause:          (id: string) => `${API_PREFIX}/nodes/${id}/pause`,
  nodeKill:           (id: string) => `${API_PREFIX}/nodes/${id}/kill`,
  allocationRestart:  (nodeId: string, allocId: string) =>
    `${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/restart`,
  allocationStop:     (nodeId: string, allocId: string) =>
    `${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/stop`,
} as const
