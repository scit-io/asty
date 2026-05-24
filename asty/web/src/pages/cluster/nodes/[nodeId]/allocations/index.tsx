import { useCallback, useRef } from 'react'
import { useParams } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { LoadingBlock } from '@/components/loading-block'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { AllocationsTable } from '@/features/allocations/allocations-table'
import { useSubscribe } from '@/lib/use-subscribe'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'
import type { Allocation } from '@/types'

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

  useSubscribe(subscribeNode, nodeId)

  // Ref so the resources callback can read the latest services list
  // without listing `services` as a useCallback dep (services ref
  // changes every SSE flush — making it a dep would re-create the
  // callback each tick and invalidate the table's memo'd columns).
  const servicesRef = useRef(services)
  servicesRef.current = services
  const resources = useCallback(
    (a: Allocation) => servicesRef.current.find((s) => s.Name === a.service_name)?.Resources,
    [],
  )

  return (
    <PageShell>
      {node && <NodeHeader node={node} tail={[{ label: 'Allocations' }]} />}
      {nodeId && <ResourceTabs items={nodeTabs(nodeId)} />}

      {!cached ? (
        <LoadingBlock />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <AllocationsTable
              rows={allocations}
              scope="node"
              resources={resources}
              emptyMessage="No allocations on this node."
              searchPlaceholder="Search by service name…"
            />
          </CardContent>
        </Card>
      )}
    </PageShell>
  )
}
