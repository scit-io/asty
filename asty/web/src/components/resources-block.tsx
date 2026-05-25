import { Tile } from '@/components/tile'
import { formatMB, formatMHz } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { Cpu, MemoryStick, HardDrive, Activity } from 'lucide-react'

interface ResourcesBlockData {
  cpuUsage: number
  cpuTotal: number
  memoryUsage: number  // MB
  memoryTotal: number  // MB
  diskUsage?: number    // MB
  diskTotal?: number    // MB
  rps?: number          // optional, undefined hides the RPS tile
}

interface ResourcesBlockProps {
  title?: string
  data: ResourcesBlockData
}

// colsClass keeps the grid column count in sync with the number of
// visible tiles so 3 tiles fill the row (not 3-of-4 with a gap).
// Literal class strings so Tailwind JIT picks them up.
const colsClass: Record<number, string> = {
  2: 'sm:grid-cols-2',
  3: 'sm:grid-cols-2 lg:grid-cols-3',
  4: 'sm:grid-cols-2 lg:grid-cols-4',
}

// ResourcesBlock renders the 4 base resource tiles (CPU/RAM/Disk/RPS)
// in a single grid. Used for the Asty/NATS sub-blocks on cluster and
// node overview pages. Disk and RPS tiles collapse out when the data
// source can't supply them; the grid column count adapts so the
// visible tiles always span the full row.
export function ResourcesBlock({ title, data }: ResourcesBlockProps) {
  const t = useT()
  const showDisk = data.diskTotal !== undefined
  const showRps = data.rps !== undefined
  const tileCount = 2 + (showDisk ? 1 : 0) + (showRps ? 1 : 0)
  return (
    <section className="space-y-3">
      {title && <h2 className="text-lg font-semibold">{title}</h2>}
      <div className={`grid gap-3 ${colsClass[tileCount]}`}>
        <Tile variant="metric" title={t('tile.cpu')} icon={<Cpu className="h-4 w-4" />}
          usage={data.cpuUsage} total={data.cpuTotal} format={formatMHz} />
        <Tile variant="metric" title={t('tile.ram')} icon={<MemoryStick className="h-4 w-4" />}
          usage={data.memoryUsage} total={data.memoryTotal} format={formatMB} />
        {showDisk && (
          <Tile variant="metric" title={t('tile.disk')} icon={<HardDrive className="h-4 w-4" />}
            usage={data.diskUsage ?? 0} total={data.diskTotal!} format={formatMB} />
        )}
        {showRps && (
          <Tile variant="stat" bar title={t('tile.rps')} icon={<Activity className="h-4 w-4" />}
            value={Math.round(data.rps!)} hint={t('common.requests_per_second')} />
        )}
      </div>
    </section>
  )
}
