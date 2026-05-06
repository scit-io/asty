import type {
  AllocationDetail,
  LogsResponse,
  AutoscalerStatusResponse,
  AutoscalerEventsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'

const API_BASE = '/api/v1'

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, options)
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`)
  }
  return response.json()
}

// Realtime data (status, nodes, services, allocations, metrics) flows through
// SSE — see /api/v1/stream*. REST endpoints below are reserved for mutations
// and the few non-streaming reads that don't fit the SSE model.
export const api = {
  // Services (definition lookup; runtime data comes from SSE)
  getService: (name: string) =>
    fetchJSON<ServiceDetailResponse>(`${API_BASE}/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`${API_BASE}/services/${name}/scale`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations (definition lookup; live data comes from SSE)
  getAllocation: (id: string) =>
    fetchJSON<AllocationDetail>(`${API_BASE}/allocations/${id}`),
  getAllocationLogs: (id: string) =>
    fetchJSON<LogsResponse>(`${API_BASE}/logs/allocation/${id}`),

  // Autoscaler (events history is not part of SSE; status mirrors SSE but
  // exposed for ad-hoc queries)
  getAutoscalerStatus: () =>
    fetchJSON<AutoscalerStatusResponse>(`${API_BASE}/autoscaler/status`),
  getAutoscalerEvents: (service?: string, limit?: number) =>
    fetchJSON<AutoscalerEventsResponse>(
      `${API_BASE}/autoscaler/events?service=${service || ''}&limit=${limit || 100}`
    ),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(`${API_BASE}/deploy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ service, version }),
    }),
  getDeployments: () =>
    fetchJSON<DeploymentsResponse>(`${API_BASE}/deployments`),

  // Mutations
  drainNode: (id: string, enable: boolean) =>
    fetchJSON(`${API_BASE}/nodes/${id}/drain`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable }),
    }),
  getDrainStatus: (id: string) =>
    fetchJSON<DrainStatus>(`${API_BASE}/nodes/${id}/drain/status`),
  pauseNode: (id: string) =>
    fetchJSON(`${API_BASE}/nodes/${id}/pause`, { method: 'POST' }),
  restartAllocation: (id: string) =>
    fetchJSON(`${API_BASE}/allocations/${id}/restart`, { method: 'POST' }),
  stopAllocation: (id: string) =>
    fetchJSON(`${API_BASE}/allocations/${id}/stop`, { method: 'POST' }),
}
