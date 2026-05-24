import { routes } from '@/lib/routes'
import type { TabItem } from '@/components/resource-tabs'

// Service-detail tabs. Rendered on every page under /services/:name —
// Overview, Allocations, Scaling events, Deploy history.
export const serviceTabs = (name: string): TabItem[] => [
  { to: routes.service(name), label: 'Overview' },
  { to: routes.serviceAllocations(name), label: 'Allocations' },
  { to: routes.serviceAutoscaler(name), label: 'Scaling events' },
  { to: routes.serviceDeploy(name), label: 'Deploy history' },
]
