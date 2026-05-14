import { MetricTile } from '@/components/metric-tile'
import { formatMB, formatMHz } from '@/lib/format'
import { Cpu, MemoryStick, HardDrive, Activity } from 'lucide-react'
import type { ReactNode } from 'react'

interface Resource {
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
  data: Resource
  // rpsChart slot lets pages plug in the existing MetricsChart with
  // their own data series; the block itself stays data-shape-agnostic.
  cpuChart?: ReactNode
  memoryChart?: ReactNode
  rpsChart?: ReactNode
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
// in a single grid. Used for the Cluster Overview tile row, the Node
// Overview tile row, the Allocation Overview tile row, and the Asty/
// NATS sub-blocks on the same pages. Disk and RPS tiles collapse out
// when the data source can't supply them; the grid column count
// adapts so the visible tiles always span the full row.
export function ResourcesBlock({ title, data, cpuChart, memoryChart, rpsChart }: ResourcesBlockProps) {
  const showDisk = data.diskTotal !== undefined
  const showRps = data.rps !== undefined
  const tileCount = 2 + (showDisk ? 1 : 0) + (showRps ? 1 : 0)
  return (
    <section className="space-y-3">
      {title && <h2 className="text-lg font-semibold">{title}</h2>}
      <div className={`grid gap-3 ${colsClass[tileCount]}`}>
        <MetricTile
          title="CPU"
          icon={<Cpu className="h-4 w-4" />}
          usage={data.cpuUsage}
          total={data.cpuTotal}
          unit=""
          format={formatMHz}
          chart={cpuChart}
        />
        <MetricTile
          title="Memory"
          icon={<MemoryStick className="h-4 w-4" />}
          usage={data.memoryUsage}
          total={data.memoryTotal}
          unit=""
          format={formatMB}
          chart={memoryChart}
        />
        {showDisk && (
          <MetricTile
            title="Disk"
            icon={<HardDrive className="h-4 w-4" />}
            usage={data.diskUsage ?? 0}
            total={data.diskTotal!}
            unit=""
            format={formatMB}
          />
        )}
        {showRps && (
          <MetricTile
            title="RPS"
            icon={<Activity className="h-4 w-4" />}
            usage={data.rps!}
            total={data.rps!}
            unit="Requests per second"
            chart={rpsChart}
          />
        )}
      </div>
    </section>
  )
}
