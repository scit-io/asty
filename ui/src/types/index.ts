export interface Node {
  id: string
  datacenter: string
  status: 'ready' | 'down' | 'initializing'
  cpu_total: number
  cpu_available: number
  memory_total: number
  memory_available: number
  processes: string[]
  last_seen: string
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
  logs: string[]
}
