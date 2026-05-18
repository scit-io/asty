import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'

// API_PREFIX is the single source of truth for the orchestrator's HTTP
// namespace on the frontend side. The backend has the matching
// `apiPrefix` constant in features/api/api.go — change both in
// lockstep when renaming. Every fetch and EventSource in the SPA
// goes through this.
//
// Moved from /metrics to /api/v1 (migration/tz §14.2) so the data
// namespace stops overlapping with the Prometheus exposition path.
// The old /metrics/* prefix remains supported on the backend for one
// cycle with a Deprecation header.
export const API_PREFIX = '/api/v1'

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, options)
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

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/restart`, { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}/stop`, { method: 'POST' }),
}
