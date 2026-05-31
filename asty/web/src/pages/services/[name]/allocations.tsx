import { useCallback, useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { LoadingBlock } from '@/components/loading-block'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { AllocationsTable } from '@/features/allocations/allocations-table'
import { useT } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useServiceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'
import type { ServiceDefinition } from '@/types'

// All allocations for a single service, regardless of node.
// AllocationsTable handles search / sort / pagination + per-row
// restart/stop actions; this page just wires the data source
// (per-service SSE) and a fixed resource lookup (same service for
// every row).
export default function ServiceAllocations() {
  const t = useT()
  const { name } = useParams<{ name: string }>()
  const tabs = useServiceTabs(name ?? '')
  const subscribeService = useClusterStore((s) => s.subscribeService)
  const cached = useClusterStore((s) => name ? s.serviceCache[name] : undefined)
  // Subscribe to each resource limit as a primitive so the selector
  // returns referentially stable values across SSE flushes (JSON.parse
  // rebuilds the Resources object every tick, but the numbers inside
  // rarely move). Reconstitute via useMemo so the `resources` callback
  // below is stable too — that is what lets AllocationsTable's memo'd
  // columns and per-cell memo actually skip work.
  const cpuLimit = useClusterStore((s) => name ? s.services.find((x) => x.Name === name)?.Resources?.CPU : undefined)
  const memLimit = useClusterStore((s) => name ? s.services.find((x) => x.Name === name)?.Resources?.Memory : undefined)
  const diskLimit = useClusterStore((s) => name ? s.services.find((x) => x.Name === name)?.Resources?.Disk : undefined)
  const res = useMemo<ServiceDefinition['Resources'] | undefined>(
    () => (cpuLimit !== undefined && memLimit !== undefined ? { CPU: cpuLimit, Memory: memLimit, Disk: diskLimit } : undefined),
    [cpuLimit, memLimit, diskLimit],
  )
  const resources = useCallback(() => res, [res])
  const allocations = cached?.allocations || []

  useSubscribe(subscribeService, name)

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: t('tabs.allocations') }]} />
      <ResourceTabs items={tabs} />
      {!cached ? (
        <LoadingBlock />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <AllocationsTable
              rows={allocations}
              scope="service"
              resources={resources}
              emptyMessage={t('allocs.empty.service')}
              searchPlaceholder={t('allocs.search.by_node')}
            />
          </CardContent>
        </Card>
      )}
    </PageShell>
  )
}
