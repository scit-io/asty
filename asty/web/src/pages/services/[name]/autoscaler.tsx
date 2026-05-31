import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { TimeStack } from '@/components/time-stack'
import { useT, translateReason } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useServiceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'
import type { ScalingEvent } from '@/types'

// Scaling events tab — reads from the same per-service cache the
// Overview's autoscaler card uses (single poller in cluster store).
export default function ServiceScalingEvents() {
  const t = useT()
  const { name } = useParams<{ name: string }>()
  const tabs = useServiceTabs(name ?? '')
  const subscribeService = useClusterStore((s) => s.subscribeService)
  const cached = useClusterStore((s) => name ? s.serviceCache[name] : undefined)
  const events = cached?.scalingEvents ?? []

  useSubscribe(subscribeService, name)

  const columns = useMemo<Column<ScalingEvent>[]>(() => [
    {
      key: 'action', label: t('scaling.col.action'),
      sort: (a, b) => a.action.localeCompare(b.action),
      render: (e) => (
        <div className="flex items-center gap-1">
          <Badge variant={e.action === 'scale_up' ? 'default' : 'secondary'}>
            {e.action === 'scale_up' ? t('action.scale_up') : t('action.scale_down')}
          </Badge>
          {e.reason.startsWith('manual:') && (
            <Badge variant="outline" className="text-[10px]">{t('action.manual')}</Badge>
          )}
        </div>
      ),
    },
    {
      key: 'reason', label: t('scaling.col.reason'),
      sort: (a, b) => a.reason.localeCompare(b.reason),
      render: (e) => <span className="text-sm">{translateReason(e.reason, t)}</span>,
    },
    {
      key: 'copies', label: t('scaling.col.copies'),
      sort: (a, b) => (a.to_count - a.from_count) - (b.to_count - b.from_count),
      render: (e) => <span>{e.from_count} → {e.to_count}</span>,
    },
    {
      key: 'node', label: t('scaling.col.node'),
      sort: (a, b) => (a.node_id ?? '').localeCompare(b.node_id ?? ''),
      render: (e) => {
        if (e.node_id) return <span className="text-xs">{e.node_id}</span>
        // Manual scale is service-scoped (multiple copies / scheduler-
        // picked placement) — surface that explicitly so an empty
        // node_id doesn't read as missing data.
        if (e.reason.startsWith('manual:')) return <span className="text-xs text-muted-foreground italic">{t('scaling.scheduler')}</span>
        return <span className="text-muted-foreground text-xs">—</span>
      },
    },
    {
      key: 'time', label: t('scaling.col.time'),
      sort: (a, b) => a.timestamp - b.timestamp,
      render: (e) => <TimeStack date={new Date(e.timestamp * 1000)} />,
    },
  ], [t])

  const search = useMemo(() => ({
    placeholder: t('scaling.search_placeholder'),
    // Match against the translated reason so typing the visible
    // Russian/English words actually filters the rows — searching the
    // raw wire string would feel broken on the Russian locale.
    match: (e: ScalingEvent, q: string) => {
      const needle = q.toLowerCase()
      return translateReason(e.reason, t).toLowerCase().includes(needle)
        || (e.node_id ?? '').toLowerCase().includes(needle)
        || e.action.toLowerCase().includes(needle)
    },
  }), [t])

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: t('tabs.scaling_events') }]} />
      <ResourceTabs items={tabs} />

      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={events}
            columns={columns}
            search={search}
            rowKey={(e) => `${e.timestamp}-${e.action}-${e.node_id ?? ''}-${e.reason}`}
            emptyMessage={t('scaling.empty')}
          />
        </CardContent>
      </Card>
    </PageShell>
  )
}
