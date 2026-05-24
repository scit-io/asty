import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { AllocationsTable } from '@/features/allocations/allocations-table'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Per-node allocations list. AllocationsTable owns search / sort /
// pagination + per-row restart/stop actions; this page just wires the
// data source (node SSE) and the per-row resource lookup.
export default function NodeAllocations() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const subscribeNode = useClusterStore((s) => s.subscribeNode)
  const services = useClusterStore((s) => s.services)
  const cached = useClusterStore((s) => nodeId ? s.nodeCache[nodeId] : undefined)
  const node = cached?.node || null
  const allocations = cached?.allocations || []

  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  return (
    <PageShell>
      {node && <NodeHeader node={node} tail={[{ label: 'Allocations' }]} />}
      {nodeId && <ResourceTabs items={nodeTabs(nodeId)} />}

      {!cached ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <AllocationsTable
              rows={allocations}
              scope="node"
              resources={(a) => services.find((s) => s.Name === a.service_name)?.Resources}
              emptyMessage="No allocations on this node."
              searchPlaceholder="Search by service name…"
            />
          </CardContent>
        </Card>
      )}
    </PageShell>
  )
}
