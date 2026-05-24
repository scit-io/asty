import { routes } from '@/lib/routes'
import type { TabItem } from '@/components/resource-tabs'

// Top-level Services section tabs. Currently a single entry — the
// global Header collapses sections with one child into a direct link.
// Lives here for symmetry with CLUSTER_TABS so the Header pulls every
// section's tabs from the same convention.
export const SERVICES_TABS: TabItem[] = [
  { to: routes.services, label: 'Overview' },
]
