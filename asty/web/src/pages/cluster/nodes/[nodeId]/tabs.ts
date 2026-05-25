import { useMemo } from 'react'
import { routes } from '@/lib/routes'
import { useT } from '@/lib/i18n'
import type { TabItem } from '@/components/resource-tabs'

// Node-detail tabs. Rendered on every page under /nodes/:id —
// Overview, Allocations, Logs — so picking a tab pushes the matching
// URL without forking the strings across pages.
export function useNodeTabs(id: string): TabItem[] {
  const t = useT()
  return useMemo(() => [
    { to: routes.node(id), label: t('tabs.overview') },
    { to: routes.nodeAllocations(id), label: t('tabs.allocations') },
    { to: routes.nodeLogs(id), label: t('tabs.logs') },
  ], [t, id])
}
