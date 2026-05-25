import { useMemo } from 'react'
import { routes } from '@/lib/routes'
import { useT } from '@/lib/i18n'
import type { TabItem } from '@/components/resource-tabs'

// Top-level Cluster section tabs. Used both by the page-shell
// ResourceTabs row on every Cluster page (Overview / Nodes / Logs)
// and by the dropdown in the global Header. Single source so the two
// surfaces never drift. Memoised on `t` so the array reference stays
// stable across SSE flushes — ResourceTabs/Header derive memos from it.
export function useClusterTabs(): TabItem[] {
  const t = useT()
  return useMemo(() => [
    { to: routes.cluster, label: t('tabs.overview') },
    { to: routes.nodes, label: t('tabs.nodes') },
    { to: routes.clusterLogs, label: t('tabs.logs') },
  ], [t])
}
