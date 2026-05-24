import { routes } from '@/lib/routes'
import type { TabItem } from '@/components/resource-tabs'

// Node-detail tabs. Rendered on every page under /nodes/:id —
// Overview, Allocations, Logs — so picking a tab pushes the matching
// URL without forking the strings across pages.
export const nodeTabs = (id: string): TabItem[] => [
  { to: routes.node(id), label: 'Overview' },
  { to: routes.nodeAllocations(id), label: 'Allocations' },
  { to: routes.nodeLogs(id), label: 'Logs' },
]
