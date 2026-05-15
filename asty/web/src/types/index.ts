export interface Node {
  id: string
  datacenter: string
  ip: string
  status: 'ready' | 'down' | 'draining' | 'drained' | 'paused'
  cpu_total: number          // MHz
  cpu_available: number      // MHz
  memory_total: number       // MB
  memory_available: number   // MB
  disk_total: number         // MB
  disk_available: number     // MB
  disk_type: 'ssd' | 'hdd' | 'unknown'
  swap_total: number         // MB
  swap_available: number     // MB
  // Asty agent process itself
  self_cpu_percent: number
  self_memory_mb: number
  self_disk_mb: number
  // Local NATS server stats (zero when nats -m 8222 isn't on)
  nats_cpu_percent: number
  nats_memory_mb: number
  nats_connections: number
  nats_subscriptions: number
  nats_slow_consumers: number
  nats_in_msgs: number
  nats_out_msgs: number
  nats_jetstream_messages: number
  nats_jetstream_bytes: number
  nats_disk_mb: number
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
  status: 'pending' | 'starting' | 'running' | 'stopped' | 'failed'
  version: string
  pid: number
  started_at: string
  health_status: 'healthy' | 'unhealthy' | 'unknown'
  cpu_usage: number         // percentage
  memory_usage: number      // MB
  disk_usage: number        // MB, on-disk size under <work_dir>/<service>
  restarts: number
  consecutive_failures: number
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
    leader_ip: string
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

  // Runtime fields populated by streamHub in the global SSE 'services' event.
  // Optional because GET /services returns the bare definition without runtime.
  current_copies?: number
  avg_cpu_percent?: number
  avg_memory_percent?: number
  avg_cpu_mhz?: number
  avg_memory_mb?: number
  min_copies?: number
  target_cpu?: number
  target_memory?: number
  traffic_threshold?: number
  cooldown_up_active?: boolean
  cooldown_down_active?: boolean
  last_action?: string
  last_action_at?: number
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

export interface MetricsResponse {
  cpu: MetricPoint[]
  memory: MetricPoint[]
  rps?: MetricPoint[]
  period: string
}

export interface ServiceMetricsResponse extends MetricsResponse {
  service: string
  allocations_count: MetricPoint[]
}

export interface AllocationMetricsResponse extends MetricsResponse {
  allocation_id: string
}

export interface ScalingEvent {
  timestamp: number
  service: string
  action: 'scale_up' | 'scale_down'
  reason: string
  from_count: number
  to_count: number
  node_id?: string
}

export interface AutoscalerServiceStatus {
  current_copies: number
  min_copies: number
  target_cpu: number
  target_memory: number
  traffic_threshold: number
  cooldown_up_active: boolean
  cooldown_down_active: boolean
  last_action: string
  last_action_at: number
}

export interface AutoscalerStatusResponse {
  services: Record<string, AutoscalerServiceStatus>
}

export interface AutoscalerEventsResponse {
  events: ScalingEvent[]
  count: number
}

export interface ServiceDetailResponse {
  service: ServiceDefinition
  allocations: Allocation[]
}

export interface DeploymentRecord {
  id: string
  service: string
  version: string
  strategy: string
  status: string
  started_at: string
  completed_at?: string
  progress: number
}

export interface DeploymentsResponse {
  deployments: DeploymentRecord[]
  count: number
}

export interface DrainStatus {
  node_id: string
  status: string // draining, drained, ready, error
  total_allocations: number
  migrated: number
  remaining: number
  current_allocation: string
  errors: string[]
}
