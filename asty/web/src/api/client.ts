import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'

// All orchestrator data lives at the root path on :8080 — content-
// negotiation per Accept header switches between JSON and SSE on the
// same URL. There is no /api/v1 prefix.
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
    fetchJSON<ServiceDetailResponse>(`/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`/services/${name}/scale`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations (now scoped under their hosting node)
  getAllocation: (nodeId: string, allocId: string) =>
    fetchJSON<AllocationDetail>(`/nodes/${nodeId}/allocations/${allocId}`),
  getAllocationLogs: (nodeId: string, allocId: string) =>
    fetchJSON<LogsResponse>(`/nodes/${nodeId}/allocations/${allocId}/logs`),

  // Autoscaler — single per-service endpoint returns status + events.
  getServiceAutoscaler: (name: string) =>
    fetchJSON(`/services/${name}/autoscaler`),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(`/services/${service}/deploy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version }),
    }),
  getServiceDeployments: (service: string) =>
    fetchJSON<DeploymentsResponse>(`/services/${service}/deploy`),

  // Node maintenance
  drainNode: (id: string, enable: boolean) =>
    fetchJSON(`/nodes/${id}/drain`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable }),
    }),
  getDrainStatus: (id: string) =>
    fetchJSON<DrainStatus>(`/nodes/${id}/drain`),
  pauseNode: (id: string) =>
    fetchJSON(`/nodes/${id}/pause`, { method: 'POST' }),

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`/nodes/${nodeId}/allocations/${allocId}/restart`, { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(`/nodes/${nodeId}/allocations/${allocId}/stop`, { method: 'POST' }),
}
