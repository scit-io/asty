import { useMemo } from 'react'
import { routes } from '@/lib/routes'
import { useT } from '@/lib/i18n'
import type { TabItem } from '@/components/resource-tabs'

// Allocation-detail tabs. Rendered on every page under
// /nodes/:id/allocations/:allocId — Overview, Logs.
export function useAllocationTabs(nodeId: string, allocId: string): TabItem[] {
  const t = useT()
  return useMemo(() => [
    { to: routes.allocation(nodeId, allocId), label: t('tabs.overview') },
    { to: routes.allocationLogs(nodeId, allocId), label: t('tabs.logs') },
  ], [t, nodeId, allocId])
}
