import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { AllocationsTable } from '@/features/allocations/allocations-table'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'

// All allocations for a single service, regardless of node.
// AllocationsTable handles search / sort / pagination + per-row
// restart/stop actions; this page just wires the data source
// (per-service SSE) and a fixed resource lookup (same service for
// every row).
export default function ServiceAllocations() {
  const { name } = useParams<{ name: string }>()
  const { serviceCache, subscribeService, services } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const allocations = cached?.allocations || []
  const res = name ? services.find((s) => s.Name === name)?.Resources : undefined

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: 'Allocations' }]} />
      <ResourceTabs items={serviceTabs(name)} />
      {!cached ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <AllocationsTable
              rows={allocations}
              scope="service"
              resources={() => res}
              emptyMessage="No allocations for this service."
              searchPlaceholder="Search by node…"
            />
          </CardContent>
        </Card>
      )}
    </PageShell>
  )
}
