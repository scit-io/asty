import type { StoreApi } from 'zustand'
import type {
  Node,
  ClusterStatus,
  MetricPoint,
  Allocation,
  ServiceDefinition,
  ScalingEvent,
  DeploymentRecord,
} from '@/types'

// AutoscalerInfo mirrors the fields the dashboard's /autoscaler
// endpoint returns alongside the events ring. Lifted into the store
// so Overview's configuration card and the Scaling events tab share
// a single poller per active service instead of each duplicating it.
export interface AutoscalerInfo {
  min_copies: number
  min_copies_default: number
  min_copies_override: boolean
  max_copies: number
  deploy_in_progress: boolean
}

export interface NodeData {
  node: Node | null
  allocations: Allocation[]
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
  rpsMetrics: MetricPoint[]
}

export interface ServiceData {
  service: ServiceDefinition | null
  allocations: Allocation[]
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
  allocCountMetrics: MetricPoint[]
  // Autoscaler + deploy data are fetched alongside the per-service
  // SSE so every view under /services/:name (Overview, Scaling
  // events, Deploy history) reads from the same cache without
  // duplicating pollers / EventSources.
  autoscaler: AutoscalerInfo | null
  scalingEvents: ScalingEvent[]
  liveDeploy: DeploymentRecord | null
  deployHistory: DeploymentRecord[]
  // Deploy-target versions pulled from the GitHub Releases of the
  // configured repo (server-side cached). Empty list in dev where
  // the artifact URL is `local`. Refetched on subscribeService.
  availableVersions: string[]
}

export interface AllocationData {
  allocation: Allocation | null
  service: ServiceDefinition | null
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
  rpsMetrics: MetricPoint[]
}

export interface ClusterStore {
  // Globals — populated by `status` / `services` events that every
  // per-resource stream emits, so any active page keeps these warm.
  clusterStatus: ClusterStatus | null
  nodes: Node[]
  services: ServiceDefinition[]

  // Cluster overview timeseries (only populated by subscribeCluster).
  clusterCpuMetrics: MetricPoint[]
  clusterMemoryMetrics: MetricPoint[]
  clusterRpsMetrics: MetricPoint[]

  // Per-resource page caches.
  nodeCache: Record<string, NodeData>
  serviceCache: Record<string, ServiceData>
  allocationCache: Record<string, AllocationData>

  // Page subscriptions — each opens exactly ONE EventSource for its
  // page. Cluster status, services list, nodes list are folded into
  // every per-resource stream so the Header stays live without a
  // parallel global subscription.
  subscribeCluster: () => () => void
  subscribeNodes: () => () => void
  subscribeServices: () => () => void
  subscribeNode: (nodeId: string) => () => void
  subscribeService: (name: string) => () => void
  subscribeAllocation: (nodeId: string, allocId: string) => () => void

  // refreshService re-fetches the autoscaler payload + deploy history
  // for one service immediately, bypassing the 15-second poll cadence.
  // Called by Overview's Set floor / Deploy buttons so the
  // Configuration card reflects the new state without waiting for the
  // next tick.
  refreshService: (name: string) => Promise<void>

  // Optimistic mutation (used by drain before SSE catches up).
  updateNodeStatus: (nodeId: string, status: Node['status']) => void
}

// Per-store setter (zustand's StoreApi.setState) — the type slices
// use to mutate the whole ClusterStore.
export type Setter = StoreApi<ClusterStore>['setState']

// Per-store batch scheduler — see makeScheduleSet in store/stream.ts.
export type Scheduler = (fn: (s: ClusterStore) => Partial<ClusterStore>) => void

// SliceDeps — everything a slice factory needs. Shared scheduleSet
// keeps SSE-driven updates coalesced across slices; attachStatusHandler
// is the common 'status'-event listener that lives in the cluster
// slice (it owns clusterStatus) but every slice opens streams that
// carry the status event.
export interface SliceDeps {
  set: Setter
  scheduleSet: Scheduler
  attachStatusHandler: (es: EventSource) => void
}
