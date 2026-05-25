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
import { useT } from '@/lib/i18n'

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
  const t = useT()
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">{t('nats.title')}</h2>
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
        <Tile variant="metric" title={t('tile.cpu')} icon={<Cpu className="h-4 w-4" />}
          usage={data.cpuUsage} total={data.cpuTotal} format={formatMHz} />
        <Tile variant="metric" title={t('tile.ram')} icon={<MemoryStick className="h-4 w-4" />}
          usage={data.memoryUsage} total={data.memoryTotal} format={formatMB} />
        <Tile variant="metric" title={t('tile.disk')} icon={<HardDrive className="h-4 w-4" />}
          usage={data.diskUsage} total={data.diskTotal} format={formatMB} />
        <Tile variant="stat" title={t('nats.connections')} icon={<Plug className="h-4 w-4" />}
          value={data.connections} hint={t('nats.hint.current_clients')} />
        <Tile variant="stat" title={t('nats.subscriptions')} icon={<Radio className="h-4 w-4" />}
          value={data.subscriptions} hint={t('nats.hint.active_subjects')} />
        <Tile variant="stat" title={t('nats.slow_consumers')} icon={<AlertTriangle className="h-4 w-4" />}
          value={data.slow} hint={t('nats.hint.lifetime_count')} />
        <Tile variant="stat" title={t('nats.incoming_messages')} icon={<ArrowDown className="h-4 w-4" />}
          value={formatCount(data.inMsgs)} hint={t('nats.hint.since_start')} />
        <Tile variant="stat" title={t('nats.outgoing_messages')} icon={<ArrowUp className="h-4 w-4" />}
          value={formatCount(data.outMsgs)} hint={t('nats.hint.since_start')} />
        <Tile variant="stat" title={t('nats.jetstream_messages')} icon={<Database className="h-4 w-4" />}
          value={formatCount(data.jsMessages)} hint={t('nats.hint.jetstream_total')} />
      </div>
    </section>
  )
}
