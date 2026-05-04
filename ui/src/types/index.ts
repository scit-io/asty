export interface Node {
  id: string
  datacenter: string
  ip: string
  status: 'ready' | 'down' | 'initializing'
  cpu_total: number
  cpu_available: number
  memory_total: number
  memory_available: number
  processes: string[]
  created_at: string
  last_seen: string
  allocations_running: number
  allocations_planned: number
}

export interface NodeDetail extends Node {
  uptime?: number
  metrics?: {
    cpu: MetricPoint[]
    memory: MetricPoint[]
  }
}

export interface Allocation {
  id: string
  service_name: string
  node_id: string
  status: 'pending' | 'running' | 'stopping' | 'stopped' | 'failed'
  version: string
  pid: number
  started_at: string
  health_status: 'healthy' | 'unhealthy' | 'unknown'
  cpu_usage: number
  memory_usage: number
  restarts: number
  created_at: string
  updated_at: string
}

export interface AllocationDetail extends Allocation {
  logs?: string[]
  metrics?: {
    cpu: MetricPoint[]
    memory: MetricPoint[]
  }
}

export interface MetricPoint {
  timestamp: number
  value: number
}

export interface ClusterStatus {
  cluster: {
    leader: string
    is_leader: boolean
    nodes_total: number
    nodes_healthy: number
  }
  services: {
    loaded: number
  }
}

export interface ServiceDefinition {
  Name: string
  Type: 'system' | 'service'
  Resources: {
    CPU: number
    Memory: number
  }
  Health: {
    Type: string
    Path: string
    Interval: string
    Timeout: string
  }
}

export interface NodesResponse {
  nodes: Node[]
  count: number
}

export interface ServicesResponse {
  services: ServiceDefinition[]
  count: number
}

export interface AllocationsResponse {
  allocations: Allocation[]
  count: number
}

export interface LogsResponse {
  allocation_id?: string
  service_name?: string
  node_id?: string
  logs: string[]
  line_count?: number
}
