import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'
import type { ScalingEvent } from '@/types'

// Autoscaler tab — live config from the global services SSE plus
// scaling-event history polled from /services/{name}/autoscaler.
export default function ServiceAutoscaler() {
  const { name } = useParams<{ name: string }>()
  const { services, subscribeService } = useClusterStore()
  const status = useMemo(() => services.find((s) => s.Name === name) || null, [name, services])
  const [events, setEvents] = useState<ScalingEvent[]>([])
  const [loading, setLoading] = useState(true)

  // Live config comes from the service SSE; scaling-events history
  // is REST-polled because backend doesn't (yet) push it.
  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  useEffect(() => {
    if (!name) return
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false
    const poll = async () => {
      try {
        const res = await api.getServiceAutoscaler(name) as { events?: ScalingEvent[] }
        if (!cancelled) setEvents(res.events || [])
      } catch { /* keep current */ }
      if (!cancelled) {
        setLoading(false)
        timer = setTimeout(poll, 15000)
      }
    }
    poll()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [name])

  if (!name) return null
  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[
        { label: 'Cluster', to: '/' },
        { label: 'Services', to: '/services' },
        { label: name, to: `/services/${name}` },
        { label: 'Autoscaler' },
      ]} />
      <ResourceTabs items={[
        { to: `/services/${name}`, label: 'Overview' },
        { to: `/services/${name}/allocations`, label: 'Allocations' },
        { to: `/services/${name}/autoscaler`, label: 'Autoscaler' },
        { to: `/services/${name}/deploy`, label: 'Deploy' },
      ]} />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          {!status ? (
            <Skeleton className="h-32 w-full" />
          ) : (
            <dl className="grid grid-cols-2 gap-y-2 text-sm">
              <dt className="text-muted-foreground">Current copies</dt><dd className="font-mono">{status.current_copies ?? 0}</dd>
              <dt className="text-muted-foreground">Min copies</dt><dd className="font-mono">{status.min_copies ?? 0}</dd>
              <dt className="text-muted-foreground">Target CPU</dt><dd className="font-mono">{status.target_cpu ?? 0}%</dd>
              <dt className="text-muted-foreground">Target Memory</dt><dd className="font-mono">{status.target_memory ?? 0}%</dd>
              <dt className="text-muted-foreground">Traffic threshold</dt><dd className="font-mono">{status.traffic_threshold ?? 0} Requests per second</dd>
              <dt className="text-muted-foreground">Cooldown</dt>
              <dd className="flex gap-2">
                {status.cooldown_up_active && <Badge variant="secondary">up</Badge>}
                {status.cooldown_down_active && <Badge variant="secondary">down</Badge>}
                {!status.cooldown_up_active && !status.cooldown_down_active && <span className="text-muted-foreground">inactive</span>}
              </dd>
              <dt className="text-muted-foreground">Last action</dt>
              <dd>{status.last_action
                ? <span>{status.last_action} · {status.last_action_at ? new Date(status.last_action_at * 1000).toLocaleString() : ''}</span>
                : <span className="text-muted-foreground">—</span>}
              </dd>
            </dl>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">Scaling events</CardTitle>
        </CardHeader>
        <CardContent>
          {loading && events.length === 0 ? (
            <Skeleton className="h-32 w-full" />
          ) : events.length === 0 ? (
            <div className="text-center text-muted-foreground py-8">No scaling events yet.</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Time</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Copies</TableHead>
                  <TableHead>Node</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((e, i) => (
                  <TableRow key={i}>
                    <TableCell className="text-sm">{new Date(e.timestamp * 1000).toLocaleString()}</TableCell>
                    <TableCell>
                      <Badge variant={e.action === 'scale_up' ? 'default' : 'secondary'}>
                        {e.action === 'scale_up' ? 'Scale Up' : 'Scale Down'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm">{e.reason}</TableCell>
                    <TableCell>{e.from_count} → {e.to_count}</TableCell>
                    <TableCell className="font-mono text-xs">{e.node_id || '—'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
