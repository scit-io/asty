import type {
  ClusterStatus,
  NodesResponse,
  ServicesResponse,
  NodeDetail,
  AllocationsResponse,
  AllocationDetail,
  LogsResponse,
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

  // Allocations
  getAllocation: (id: string) =>
    fetchJSON<AllocationDetail>(`${API_BASE}/allocations/${id}`),
  getAllocationLogs: (id: string) =>
    fetchJSON<LogsResponse>(`${API_BASE}/logs/allocation/${id}`),

  // Actions
  drainNode: (id: string) =>
    fetchJSON(`${API_BASE}/nodes/${id}/drain`, { method: 'POST' }),
  pauseNode: (id: string) =>
    fetchJSON(`${API_BASE}/nodes/${id}/pause`, { method: 'POST' }),
  restartAllocation: (id: string) =>
    fetchJSON(`${API_BASE}/allocations/${id}/restart`, { method: 'POST' }),
  stopAllocation: (id: string) =>
    fetchJSON(`${API_BASE}/allocations/${id}/stop`, { method: 'POST' }),
}
