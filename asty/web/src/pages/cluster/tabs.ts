import { routes } from '@/lib/routes'
import type { TabItem } from '@/components/resource-tabs'

// Top-level Cluster section tabs. Used both by the page-shell
// ResourceTabs row on every Cluster page (Overview / Nodes / Logs)
// and by the dropdown in the global Header. Single source so the two
// surfaces never drift.
export const CLUSTER_TABS: TabItem[] = [
  { to: routes.cluster, label: 'Overview' },
  { to: routes.nodes, label: 'Nodes' },
  { to: routes.clusterLogs, label: 'Logs' },
]
