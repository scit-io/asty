import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'

// API_BASE is the single source of truth for the orchestrator's HTTP
// namespace on the frontend side. The backend has the matching
// `apiPrefix` constant in features/api/api.go — change both if you
// rename. Every fetch and EventSource in the SPA goes through this.
export const API_BASE = '/api/v1'

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
    fetchJSON<ServiceDetailResponse>(`${API_BASE}/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`${API_BASE}/services/${name}/scale`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations (now scoped under their hosting node)
  getAllocation: (nodeId: string, allocId: string) =>
    fetchJSON<AllocationDetail>(`${API_BASE}/nodes/${nodeId}/allocations/${allocId}`),
  getAllocationLogs: (nodeId: string, allocId: string) =>
    fetchJSON<LogsResponse>(`${API_BASE}/nodes/${nodeId}/allocations/${allocId}/logs`),

  // Autoscaler — single per-service endpoint returns status + events.
  getServiceAutoscaler: (name: string) =>
    fetchJSON(`${API_BASE}/services/${name}/autoscaler`),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(`${API_BASE}/services/${service}/deploy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version }),
    }),
  getServiceDeployments: (service: string) =>
    fetchJSON<DeploymentsResponse>(`${API_BASE}/services/${service}/deploy`),

  // Node maintenance
  drainNode: (id: string, enable: boolean) =>
    fetchJSON(`${API_BASE}/nodes/${id}/drain`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable }),
    }),
  getDrainStatus: (id: string) =>
    fetchJSON<DrainStatus>(`${API_BASE}/nodes/${id}/drain`),
  pauseNode: (id: string) =>
    fetchJSON(`${API_BASE}/nodes/${id}/pause`, { method: 'POST' }),

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_BASE}/nodes/${nodeId}/allocations/${allocId}/restart`, { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`${API_BASE}/nodes/${nodeId}/allocations/${allocId}/stop`, { method: 'POST' }),
}
