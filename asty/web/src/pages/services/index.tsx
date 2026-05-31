import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { UsageCell } from '@/components/usage-cell'
import { Cpu, MemoryStick } from 'lucide-react'
import { formatMB, formatMHz } from '@/lib/format'
import { routes } from '@/lib/routes'
import { useT, serviceTypeKey, deployStatusKey } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useClusterStore } from '@/store/cluster'
import type { ServiceDefinition } from '@/types'

// Services list (/services). Backs onto useClusterStore().services
// which already carries the runtime ServiceWithUsage shape from the
// snapshot SSE.
export default function Services() {
  const t = useT()
  const navigate = useNavigate()
  const subscribeServices = useClusterStore((s) => s.subscribeServices)
  const services = useClusterStore((s) => s.services)

  useSubscribe(subscribeServices)

  const columns = useMemo<Column<ServiceDefinition>[]>(() => [
    {
      key: 'name', label: t('services.col.service'),
      sort: (a, b) => a.Name.localeCompare(b.Name),
      render: (s) => <span className="font-semibold">{s.Name}</span>,
    },
    {
      key: 'type', label: t('services.col.type'),
      sort: (a, b) => a.Type.localeCompare(b.Type),
      render: (s) => <Badge variant={s.Type === 'system' ? 'secondary' : 'default'}>{t(serviceTypeKey(s.Type))}</Badge>,
    },
    {
      key: 'copies', label: t('services.col.copies'),
      sort: (a, b) => (a.current_copies ?? 0) - (b.current_copies ?? 0),
      render: (s) => s.Type === 'service'
        ? <span><b>{s.current_copies ?? 0}</b> / {t('services.copies.min', { n: s.min_copies ?? 0 })}</span>
        : <span className="text-muted-foreground">{s.current_copies ?? 0}</span>,
    },
    {
      key: 'cpu', label: t('services.col.cpu_per_copy'),
      sort: (a, b) => (a.avg_cpu_percent ?? 0) - (b.avg_cpu_percent ?? 0),
      render: (s) => {
        const pct = Math.round(s.avg_cpu_percent ?? 0)
        const mhz = Math.round(((s.avg_cpu_percent ?? 0) / 100) * s.Resources.CPU)
        return <UsageCell icon={Cpu} primary={`${pct}%`} secondary={`${formatMHz(mhz)} / ${formatMHz(s.Resources.CPU)}`} />
      },
    },
    {
      key: 'mem', label: t('services.col.ram_per_copy'),
      sort: (a, b) => (a.avg_memory_mb ?? 0) - (b.avg_memory_mb ?? 0),
      render: (s) => {
        const used = s.avg_memory_mb ?? 0
        const pct = s.Resources.Memory > 0 ? Math.round((used / s.Resources.Memory) * 100) : null
        return (
          <UsageCell
            icon={MemoryStick}
            primary={pct !== null ? `${pct}%` : formatMB(used)}
            secondary={`${formatMB(used)} / ${formatMB(s.Resources.Memory)}`}
          />
        )
      },
    },
    {
      key: 'cooldown', label: t('services.col.cooldown'),
      render: (s) => (
        <div className="flex gap-1">
          {s.cooldown_up_active && <Badge variant="secondary">{t('cooldown.up')}</Badge>}
          {s.cooldown_down_active && <Badge variant="secondary">{t('cooldown.down')}</Badge>}
          {!s.cooldown_up_active && !s.cooldown_down_active && <span className="text-muted-foreground text-xs">—</span>}
        </div>
      ),
    },
    {
      key: 'last', label: t('services.col.last_action'),
      sort: (a, b) => Math.max(a.last_action_at ?? 0, a.last_deploy_at ?? 0)
        - Math.max(b.last_action_at ?? 0, b.last_deploy_at ?? 0),
      render: (s) => {
        const scaleTs = s.last_action_at ?? 0
        const deployTs = s.last_deploy_at ?? 0
        const showDeploy = !!s.last_deploy_version && deployTs >= scaleTs
        if (showDeploy) {
          const status = s.last_deploy_status ?? ''
          const variant = status === 'failed' || status === 'rollback_failed' ? 'destructive'
            : status === 'reverted' ? 'secondary'
            : 'default'
          return (
            <span className="inline-flex items-center gap-1 text-xs">
              <span>{t('action.deploy')} <span>{s.last_deploy_version}</span></span>
              <Badge variant={variant} className="text-[10px]">{t(deployStatusKey(status))}</Badge>
              <span className="text-muted-foreground">·</span>
              <span>{new Date(deployTs * 1000).toLocaleTimeString()}</span>
            </span>
          )
        }
        if (s.last_action) return (
          <span className="inline-flex items-center gap-1 text-xs">
            <span>{s.last_action === 'scale_up' ? t('action.scale_up_short') : t('action.scale_down_short')}</span>
            {s.last_reason?.startsWith('manual:') && (
              <Badge variant="outline" className="text-[10px]">{t('action.manual')}</Badge>
            )}
            <span className="text-muted-foreground">·</span>
            <span>{scaleTs ? new Date(scaleTs * 1000).toLocaleTimeString() : ''}</span>
          </span>
        )
        return <span className="text-muted-foreground text-xs">—</span>
      },
    },
  ], [t])

  const search = useMemo(() => ({
    placeholder: t('services.search_placeholder'),
    match: (s: ServiceDefinition, q: string) => s.Name.toLowerCase().includes(q.toLowerCase()),
  }), [t])

  return (
    <PageShell>
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">{t('services.title')}</h2>
        <Card>
          <CardContent className="pt-6">
            <DataTable
              rows={services}
              columns={columns}
              search={search}
              onRowClick={(s) => navigate(routes.service(s.Name))}
              rowKey={(s) => s.Name}
              emptyMessage={t('services.empty')}
            />
          </CardContent>
        </Card>
      </section>
    </PageShell>
  )
}
