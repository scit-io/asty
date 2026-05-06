import type {
  ClusterStatus,
  NodesResponse,
  ServicesResponse,
  NodeDetail,
  AllocationsResponse,
  AllocationDetail,
  LogsResponse,
  MetricsResponse,
  ServiceMetricsResponse,
  AllocationMetricsResponse,
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

export const api = {
  // Cluster
  getStatus: () => fetchJSON<ClusterStatus>(`${API_BASE}/status`),

  // Nodes
  getNodes: () => fetchJSON<NodesResponse>(`${API_BASE}/nodes`),
  getNode: (id: string) => fetchJSON<NodeDetail>(`${API_BASE}/nodes/${id}`),
  getNodeAllocations: (id: string) =>
    fetchJSON<AllocationsResponse>(`${API_BASE}/allocations?node_id=${id}`),
  getNodeLogs: (id: string) =>
    fetchJSON<LogsResponse>(`${API_BASE}/logs/node/${id}`),

  // Services
  getServices: () => fetchJSON<ServicesResponse>(`${API_BASE}/services`),
  getService: (name: string) =>
    fetchJSON<ServiceDetailResponse>(`${API_BASE}/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`${API_BASE}/services/${name}/scale`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations
  getAllocation: (id: string) =>
    fetchJSON<AllocationDetail>(`${API_BASE}/allocations/${id}`),
  getAllocationLogs: (id: string) =>
    fetchJSON<LogsResponse>(`${API_BASE}/logs/allocation/${id}`),

  // Metrics
  getClusterMetrics: (period?: string) =>
    fetchJSON<MetricsResponse>(`${API_BASE}/metrics/cluster?period=${period || '1h'}`),
  getNodeMetrics: (id: string, period?: string) =>
    fetchJSON<MetricsResponse>(`${API_BASE}/metrics/nodes/${id}?period=${period || '1h'}`),
  getServiceMetrics: (name: string, period?: string) =>
    fetchJSON<ServiceMetricsResponse>(`${API_BASE}/metrics/services/${name}?period=${period || '1h'}`),
  getAllocationMetrics: (id: string, period?: string) =>
    fetchJSON<AllocationMetricsResponse>(`${API_BASE}/metrics/allocations/${id}?period=${period || '1h'}`),

  // Autoscaler
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

  // Actions
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
