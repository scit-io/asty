import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Clock,
  Cpu,
  Database,
  HardDrive,
  Layers,
  MemoryStick,
  Plug,
  Radio,
  Signal,
  Skull,
  Wrench,
} from 'lucide-react'
import { toast } from 'sonner'
import { formatCount, formatMB, formatMHz } from '@/lib/format'
import { nodeStatusSwitchClass } from '@/lib/node-status'
import { MetricsChart } from '@/components/metrics-chart'
import { NodeDrainDialog } from '@/components/node-drain-dialog'
import { NodeHeader } from '@/components/node-header'
import { NodeKillDialog } from '@/components/node-kill-dialog'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { Tile } from '@/components/tile'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Node Overview (/nodes/:id) — first tab of the node section.
// Maintenance section (drain switch) + Asty/NATS sub-blocks land
// here; allocations and logs moved to their own routes.
export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const { nodeCache, nodes, subscribeNode, updateNodeStatus } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const isLastNode = nodes.length <= 1
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const [showDrainDialog, setShowDrainDialog] = useState(false)
  const [showKillDialog, setShowKillDialog] = useState(false)

  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  const handleDrain = async (enable: boolean) => {
    if (!nodeId) return
    try {
      await api.drainNode(nodeId, enable)
      updateNodeStatus(nodeId, enable ? 'draining' : 'ready')
      toast.success(enable ? 'Draining node' : 'Node resumed')
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    }
  }

  const handleKill = async () => {
    if (!nodeId) return
    try {
      await api.killNode(nodeId, nodeId)
      toast.success(`Node ${nodeId} killed`)
      navigate(routes.nodes)
    } catch (err) {
      toast.error(`Kill failed: ${err instanceof Error ? err.message : 'unknown'}`)
      throw err
    }
  }

  if (!node) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <Skeleton className="h-8 w-64 mb-4" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  const rps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  const draining = node.status === 'draining' || node.status === 'drained'
  const diskUnit = node.disk_type === 'ssd' || node.disk_type === 'hdd' ? node.disk_type.toUpperCase() : ''
  const drainHint = node.status === 'ready' ? 'Normal'
    : node.status === 'draining' ? 'Migrating…'
    : node.status === 'drained' ? 'Drained'
    : node.status

  // hasAsty gates only the agent sub-block — Asty is always present on
  // a running agent, but we still hold rendering until at least one
  // non-zero sample so a fresh node doesn't paint an all-zero block
  // before the first heartbeat. NATS metrics on the other hand always
  // render: an all-zeros reading just means the section is briefly
  // empty (no data yet, or stream just reconnecting), and the zeros
  // themselves are honest "we measured zero" values.
  const hasAsty = node.self_cpu_percent > 0 || node.self_memory_mb > 0 || node.self_disk_mb > 0

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <NodeHeader node={node} />

      <ResourceTabs items={nodeTabs(node.id)} />

      <section className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={node.cpu_total - node.cpu_available} total={node.cpu_total} format={formatMHz} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={node.memory_total - node.memory_available} total={node.memory_total} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={node.disk_total - node.disk_available} total={node.disk_total}
            unit={diskUnit} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" bar
            title="RPS" icon={<Activity className="h-4 w-4" />}
            value={Math.round(rps)} hint="Requests per second" />

          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node CPU" data={cpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node Memory" data={memoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Allocations" icon={<Layers className="h-4 w-4" />}
            value={`${node.allocations_running} / ${node.allocations_planned}`} hint="running / planned" />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title="Started At" icon={<Clock className="h-4 w-4" />}
            timestamp={node.created_at} />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title="Last Heartbeat" icon={<Signal className="h-4 w-4" />}
            timestamp={node.last_seen} />
          <Tile className="col-span-6 lg:col-span-3" variant="actions"
            title="Maintenance" icon={<Wrench className="h-4 w-4" />}
            actions={
              <div className="flex items-center justify-between w-full">
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-bold">Drain</span>
                    <Switch
                      checked={draining}
                      className={nodeStatusSwitchClass(node.status)}
                      onCheckedChange={(checked) => checked ? setShowDrainDialog(true) : handleDrain(false)}
                      disabled={node.status === 'draining'}
                    />
                  </div>
                  <span className="text-xs text-muted-foreground leading-none">{drainHint}</span>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setShowKillDialog(true)}
                  title="Abrupt decommission — use Drain for routine operations"
                >
                  <Skull />
                  Kill
                </Button>
              </div>
            } />
        </div>
      </section>

      {hasAsty && (
        <ResourcesBlock title="Asty"
          data={{
            cpuUsage: node.self_cpu_percent, cpuTotal: 100,
            memoryUsage: node.self_memory_mb, memoryTotal: node.memory_total,
            diskUsage: node.self_disk_mb, diskTotal: node.disk_total,
          }} />
      )}

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">NATS</h2>
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
          <Tile variant="metric" title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={node.nats_cpu_percent} total={100} format={formatMHz} />
          <Tile variant="metric" title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={node.nats_memory_mb} total={node.memory_total} format={formatMB} />
          <Tile variant="metric" title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={node.nats_disk_mb} total={node.disk_total} format={formatMB} />
          <Tile variant="stat" title="Connections" icon={<Plug className="h-4 w-4" />}
            value={node.nats_connections} hint="current clients" />
          <Tile variant="stat" title="Subscriptions" icon={<Radio className="h-4 w-4" />}
            value={node.nats_subscriptions} hint="active subjects" />
          <Tile variant="stat" title="Slow Consumers" icon={<AlertTriangle className="h-4 w-4" />}
            value={node.nats_slow_consumers} hint="lifetime count" />
          <Tile variant="stat" title="Incoming Messages" icon={<ArrowDown className="h-4 w-4" />}
            value={formatCount(node.nats_in_msgs)} hint="since NATS start" />
          <Tile variant="stat" title="Outgoing Messages" icon={<ArrowUp className="h-4 w-4" />}
            value={formatCount(node.nats_out_msgs)} hint="since NATS start" />
          <Tile variant="stat" title="JetStream Messages" icon={<Database className="h-4 w-4" />}
            value={formatCount(node.nats_jetstream_messages)} hint="JetStream total" />
        </div>
      </section>

      <NodeDrainDialog
        open={showDrainDialog}
        nodeId={node.id}
        onOpenChange={setShowDrainDialog}
        onConfirm={() => { setShowDrainDialog(false); handleDrain(true) }}
      />
      <NodeKillDialog
        open={showKillDialog}
        nodeId={node.id}
        isLastNode={isLastNode}
        onOpenChange={setShowKillDialog}
        onConfirm={handleKill}
      />
    </div>
  )
}
