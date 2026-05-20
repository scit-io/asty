import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'

// API_PREFIX is the single source of truth for the dashboard's HTTP
// namespace on the frontend side. The backend default is
// /dashboard/v1; configurable per deployment via A_DASHBOARD_PREFIX
// on the orchestrator. Every fetch and EventSource in the SPA goes
// through this.
//
// /metrics is reserved for Prometheus exposition; /api/v1 is the
// gateway entry point for user traffic and is NOT used by the SPA.
export const API_PREFIX = '/dashboard/v1'

// authToken returns the Bearer token the client attaches to write
// requests. We pull it from VITE_ASTY_TOKEN at build time and from
// window.__ASTY_TOKEN__ at runtime (set by an inline script in
// index.html before the bundle loads). Reads happen per request so a
// future rotate-without-reload becomes a one-liner.
function authToken(): string {
  if (typeof window !== 'undefined' && (window as { __ASTY_TOKEN__?: string }).__ASTY_TOKEN__) {
    return (window as { __ASTY_TOKEN__?: string }).__ASTY_TOKEN__ as string
  }
  return (import.meta.env?.VITE_ASTY_TOKEN as string) ?? ''
}

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const headers = new Headers(options?.headers)
  const token = authToken()
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(url, { ...options, headers })
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`)
  }
  return response.json()
}

export const api = {
  // Services
  getService: (name: string) =>
    fetchJSON<ServiceDetailResponse>(`${API_PREFIX}/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`${API_PREFIX}/services/${name}/scale`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations (now scoped under their hosting node)
  getAllocation: (nodeId: string, allocId: string) =>
    fetchJSON<AllocationDetail>(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}`),
  getAllocationLogs: (nodeId: string, allocId: string) =>
    fetchJSON<LogsResponse>(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/logs`),

  // Autoscaler — single per-service endpoint returns status + events.
  getServiceAutoscaler: (name: string) =>
    fetchJSON(`${API_PREFIX}/services/${name}/autoscaler`),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(`${API_PREFIX}/services/${service}/deploy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version }),
    }),
  getServiceDeployments: (service: string) =>
    fetchJSON<DeploymentsResponse>(`${API_PREFIX}/services/${service}/deploy`),

  // Node maintenance
  drainNode: (id: string, enable: boolean) =>
    fetchJSON(`${API_PREFIX}/nodes/${id}/drain`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable }),
    }),
  getDrainStatus: (id: string) =>
    fetchJSON<DrainStatus>(`${API_PREFIX}/nodes/${id}/drain`),
  pauseNode: (id: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${id}/pause`, { method: 'POST' }),
  // killNode is the abrupt-decommission counterpart of drain. Body
  // carries `confirm_name` (must equal `id`); backend triggers the
  // agent's graceful self-shutdown via NATS and force-purges any KV
  // residue. Use Drain for normal operations — see Kill dialog copy.
  killNode: (id: string, confirmName: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${id}/kill`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirm_name: confirmName }),
    }),

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/restart`, { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/stop`, { method: 'POST' }),
}
