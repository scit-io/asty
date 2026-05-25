import { useMemo } from 'react'
import { routes } from '@/lib/routes'
import { useT } from '@/lib/i18n'
import type { TabItem } from '@/components/resource-tabs'

// Service-detail tabs. Rendered on every page under /services/:name —
// Overview, Allocations, Scaling events, Deploy history.
export function useServiceTabs(name: string): TabItem[] {
  const t = useT()
  return useMemo(() => [
    { to: routes.service(name), label: t('tabs.overview') },
    { to: routes.serviceAllocations(name), label: t('tabs.allocations') },
    { to: routes.serviceAutoscaler(name), label: t('tabs.scaling_events') },
    { to: routes.serviceDeploy(name), label: t('tabs.deploy_history') },
  ], [t, name])
}
