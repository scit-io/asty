import { Badge } from '@/components/ui/badge'
import { TimeStack } from '@/components/time-stack'
import { useT, deployStatusKey } from '@/lib/i18n'
import type { DeploymentRecord, ScalingEvent, ServiceDefinition } from '@/types'

interface ServiceLastActionCellProps {
  // The freshest deploy record (deployHistory[0]) and scaling event
  // (scalingEvents[0]) from the per-service cache. Whichever has the
  // later timestamp wins; runtime.last_action acts as a final
  // fallback when neither ring has entries yet (fresh services).
  latestDeploy: DeploymentRecord | null
  latestEvent: ScalingEvent | null
  runtime: ServiceDefinition | null
}

// ServiceLastActionCell renders the "Last action" row content inside
// the Service Overview's Configuration card. Three exclusive
// branches — deploy / autoscale-or-manual scale / runtime fallback —
// chosen by recency. Each branch shows the action + an optional
// badge + a TimeStack in compact form.
export function ServiceLastActionCell({ latestDeploy, latestEvent, runtime }: ServiceLastActionCellProps) {
  const t = useT()
  const deployTs = latestDeploy ? new Date(latestDeploy.started_at).getTime() : 0
  const eventTs = latestEvent ? latestEvent.timestamp * 1000 : 0

  if (latestDeploy && deployTs >= eventTs) {
    const d = new Date(latestDeploy.started_at)
    const variant = latestDeploy.status === 'failed' || latestDeploy.status === 'rollback_failed'
      ? 'destructive'
      : latestDeploy.status === 'reverted' ? 'secondary' : 'default'
    return (
      <span className="inline-flex items-center gap-2 justify-end">
        <span>{t('action.deploy')} <span>{latestDeploy.version}</span></span>
        <Badge variant={variant} className="text-[10px]">{t(deployStatusKey(latestDeploy.status))}</Badge>
        <span className="text-muted-foreground">·</span>
        <TimeStack date={d} compact />
      </span>
    )
  }

  if (latestEvent) {
    const d = new Date(latestEvent.timestamp * 1000)
    return (
      <span className="inline-flex items-center gap-2 justify-end">
        <span>{latestEvent.action === 'scale_up' ? t('action.scale_up_short') : t('action.scale_down_short')}</span>
        {latestEvent.reason?.startsWith('manual:') && (
          <Badge variant="outline" className="text-[10px]">{t('action.manual')}</Badge>
        )}
        <span className="text-muted-foreground">·</span>
        <TimeStack date={d} compact />
      </span>
    )
  }

  if (runtime?.last_action) {
    const d = runtime.last_action_at ? new Date(runtime.last_action_at * 1000) : null
    return (
      <span>
        {runtime.last_action}
        {d && <> · <TimeStack date={d} compact /></>}
      </span>
    )
  }

  return <span className="text-muted-foreground">—</span>
}
