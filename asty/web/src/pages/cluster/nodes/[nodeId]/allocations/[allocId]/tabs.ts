import { routes } from '@/lib/routes'
import type { TabItem } from '@/components/resource-tabs'

// Allocation-detail tabs. Rendered on every page under
// /nodes/:id/allocations/:allocId — Overview, Logs.
export const allocationTabs = (nodeId: string, allocId: string): TabItem[] => [
  { to: routes.allocation(nodeId, allocId), label: 'Overview' },
  { to: routes.allocationLogs(nodeId, allocId), label: 'Logs' },
]
