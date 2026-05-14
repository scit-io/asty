import { MetricTile } from '@/components/metric-tile'
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

const mbFormat = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(1)}G` : `${mb}M`)

// ResourcesBlock renders the 4 base resource tiles (CPU/RAM/Disk/RPS)
// in a single grid. Used for the Cluster Overview tile row, the Node
// Overview tile row, the Allocation Overview tile row, and the Asty/
// NATS sub-blocks on the same pages. RPS tile collapses out when the
// data source can't supply it (per-alloc, per-service).
export function ResourcesBlock({ title, data, cpuChart, memoryChart, rpsChart }: ResourcesBlockProps) {
  return (
    <section className="space-y-3">
      {title && <h2 className="text-lg font-semibold">{title}</h2>}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <MetricTile
          title="CPU"
          icon={<Cpu className="h-4 w-4" />}
          usage={data.cpuUsage}
          total={data.cpuTotal}
          unit="MHz"
          chart={cpuChart}
        />
        <MetricTile
          title="Memory"
          icon={<MemoryStick className="h-4 w-4" />}
          usage={data.memoryUsage}
          total={data.memoryTotal}
          unit=""
          format={mbFormat}
          chart={memoryChart}
        />
        {data.diskTotal !== undefined && (
          <MetricTile
            title="Disk"
            icon={<HardDrive className="h-4 w-4" />}
            usage={data.diskUsage ?? 0}
            total={data.diskTotal}
            unit=""
            format={mbFormat}
          />
        )}
        {data.rps !== undefined && (
          <MetricTile
            title="RPS"
            icon={<Activity className="h-4 w-4" />}
            usage={data.rps}
            total={data.rps}
            unit="rps"
            chart={rpsChart}
          />
        )}
      </div>
    </section>
  )
}
