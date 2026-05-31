import type { ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { LoadingBlock } from '@/components/loading-block'
import { Settings2 } from 'lucide-react'
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import type { AutoscalerInfo } from '@/store/types'
import type { DeploymentRecord, ScalingEvent, ServiceDefinition } from '@/types'
import { ServiceLastActionCell } from './service-last-action-cell'

interface ServiceConfigCardProps {
  // runtime carries every per-service tunable the dashboard reads
  // from the snapshot SSE (current_copies, min_copies, target_*,
  // cooldown flags, …). null while the SSE hasn't delivered the
  // service object yet — card renders a Skeleton in that window.
  runtime: ServiceDefinition | null
  // autoscaler payload from the /autoscaler REST poll. Carries the
  // floor override + max_copies + the deploy_in_progress gate flag.
  autoscaler: AutoscalerInfo | null
  latestDeploy: DeploymentRecord | null
  latestEvent: ScalingEvent | null
  className?: string
}

// Row collapses the 16 inline `<TableCell className="...">` repeats
// that the Configuration table used to carry. label is the muted
// left column; value is the right-aligned content. valueClass is the
// per-row override callers pass in for tighter typography (e.g. the
// Last action row that has to fit a deploy/scale-action + Badge +
// TimeStack on one line).
function Row({ label, value, valueClass = '' }: { label: string; value: ReactNode; valueClass?: string }) {
  return (
    <TableRow>
      <TableCell className="text-muted-foreground px-0 py-2">{label}</TableCell>
      <TableCell className={cn('text-right px-0 py-2', valueClass)}>{value}</TableCell>
    </TableRow>
  )
}

// ServiceConfigCard owns the Configuration card on /services/:name.
// 8 rows of read-only tunables + cooldown badge stack + Last action.
// Source of truth for the per-service runtime values; mutation lives
// in the sibling Min copies / Deploy cards.
export function ServiceConfigCard({ runtime, autoscaler, latestDeploy, latestEvent, className }: ServiceConfigCardProps) {
  const t = useT()
  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{t('svc.config.title')}</CardTitle>
        <Settings2 className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        {!runtime ? (
          <LoadingBlock />
        ) : (
          <Table>
            <TableBody>
              <Row label={t('svc.config.current_copies')} value={runtime.current_copies ?? 0} />
              <Row label={t('svc.config.min_copies')} value={
                <span className="inline-flex items-center gap-2 justify-end">
                  {autoscaler?.min_copies ?? runtime.min_copies ?? 0}
                  {autoscaler?.min_copies_override && (
                    <Badge variant="secondary" className="font-sans text-[10px]">
                      {t('svc.config.overridden_default', { n: autoscaler.min_copies_default })}
                    </Badge>
                  )}
                </span>
              } />
              <Row label={t('svc.config.max_copies')} value={
                autoscaler?.max_copies && autoscaler.max_copies > 0 ? autoscaler.max_copies : t('svc.config.unlimited')
              } />
              <Row label={t('svc.config.target_cpu')} value={`${runtime.target_cpu ?? 0}%`} />
              <Row label={t('svc.config.target_ram')} value={`${runtime.target_memory ?? 0}%`} />
              <Row label={t('svc.config.traffic_threshold')} value={`${runtime.traffic_threshold ?? 0} RPS`} />
              <Row label={t('svc.config.cooldown')} value={
                <span className="inline-flex gap-1 justify-end">
                  {runtime.cooldown_up_active && <Badge variant="secondary">{t('cooldown.up')}</Badge>}
                  {runtime.cooldown_down_active && <Badge variant="secondary">{t('cooldown.down')}</Badge>}
                  {autoscaler?.deploy_in_progress && <Badge variant="secondary">{t('cooldown.deploy')}</Badge>}
                  {!runtime.cooldown_up_active && !runtime.cooldown_down_active && !autoscaler?.deploy_in_progress &&
                    <span className="text-muted-foreground">{t('cooldown.inactive')}</span>}
                </span>
              } />
              <Row valueClass="text-sm" label={t('svc.config.last_action')} value={
                <ServiceLastActionCell
                  latestDeploy={latestDeploy}
                  latestEvent={latestEvent}
                  runtime={runtime}
                />
              } />
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
