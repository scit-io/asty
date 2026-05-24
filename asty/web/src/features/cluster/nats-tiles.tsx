import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Cpu,
  Database,
  HardDrive,
  MemoryStick,
  Plug,
  Radio,
} from 'lucide-react'
import { Tile } from '@/components/tile'
import { formatCount, formatMB, formatMHz } from '@/lib/format'

export interface NatsData {
  cpuUsage: number
  cpuTotal: number
  memoryUsage: number
  memoryTotal: number
  diskUsage: number
  diskTotal: number
  connections: number
  subscriptions: number
  slow: number
  inMsgs: number
  outMsgs: number
  jsMessages: number
}

// NatsTiles renders the 9-tile NATS metrics grid shown on both
// cluster overview (aggregates across nodes) and per-node overview
// (one node's stats). Layout and copy stay byte-identical; only the
// data source differs.
export function NatsTiles({ data }: { data: NatsData }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">NATS</h2>
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
        <Tile variant="metric" title="CPU" icon={<Cpu className="h-4 w-4" />}
          usage={data.cpuUsage} total={data.cpuTotal} format={formatMHz} />
        <Tile variant="metric" title="Memory" icon={<MemoryStick className="h-4 w-4" />}
          usage={data.memoryUsage} total={data.memoryTotal} format={formatMB} />
        <Tile variant="metric" title="Disk" icon={<HardDrive className="h-4 w-4" />}
          usage={data.diskUsage} total={data.diskTotal} format={formatMB} />
        <Tile variant="stat" title="Connections" icon={<Plug className="h-4 w-4" />}
          value={data.connections} hint="current clients" />
        <Tile variant="stat" title="Subscriptions" icon={<Radio className="h-4 w-4" />}
          value={data.subscriptions} hint="active subjects" />
        <Tile variant="stat" title="Slow Consumers" icon={<AlertTriangle className="h-4 w-4" />}
          value={data.slow} hint="lifetime count" />
        <Tile variant="stat" title="Incoming Messages" icon={<ArrowDown className="h-4 w-4" />}
          value={formatCount(data.inMsgs)} hint="since NATS start" />
        <Tile variant="stat" title="Outgoing Messages" icon={<ArrowUp className="h-4 w-4" />}
          value={formatCount(data.outMsgs)} hint="since NATS start" />
        <Tile variant="stat" title="JetStream Messages" icon={<Database className="h-4 w-4" />}
          value={formatCount(data.jsMessages)} hint="JetStream total" />
      </div>
    </section>
  )
}
