import { useClusterStore } from '@/store/cluster'
import { useT } from '@/lib/i18n'
import { NodeIdentityTooltip } from '@/components/node-identity-tooltip'

// Footer shows which node currently answers the dashboard. The value is
// served_by from the cluster status (the node that produced the latest
// snapshot), so it's accurate even when the SPA reaches the cluster
// through a multi-A-record domain and the browser picked one of several
// nodes. While the live feed is down clusterStatus is null — we show a
// reconnecting hint instead of a stale node id.
//
// A globe tooltip next to the id surfaces the node's dc/ip/host — same
// component the cluster page leader tile uses, so identity tooltips
// across the dashboard render and behave identically.
export function Footer() {
  const t = useT()
  const servedBy = useClusterStore((s) => s.clusterStatus?.cluster.served_by)
  const servedByNode = useClusterStore((s) =>
    servedBy ? s.nodes.find((n) => n.id === servedBy) : undefined
  )

  return (
    <footer
      style={{ right: 'var(--scrollbar-width, 0px)' }}
      className="absolute bottom-0 z-10 inline-flex items-center gap-1.5 rounded-tl-lg bg-muted/60 px-3 py-1.5 text-[11px] leading-none text-muted-foreground/70"
    >
      {servedBy ? (
        <>
          <span>{t('footer.connected_to', { node: servedBy })}</span>
          <NodeIdentityTooltip
            dc={servedByNode?.datacenter}
            ip={servedByNode?.ip}
            host={servedByNode?.host}
            iconClassName="h-3 w-3 opacity-60" />
        </>
      ) : (
        t('footer.disconnected')
      )}
    </footer>
  )
}
