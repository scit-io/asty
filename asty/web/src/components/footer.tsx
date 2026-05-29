import { useClusterStore } from '@/store/cluster'
import { useT } from '@/lib/i18n'

// Footer shows which node currently answers the dashboard. The value is
// served_by from the cluster status (the node that produced the latest
// snapshot), so it's accurate even when the SPA reaches the cluster
// through a multi-A-record domain and the browser picked one of several
// nodes. While the live feed is down clusterStatus is null — we show a
// reconnecting hint instead of a stale node id.
export function Footer() {
  const t = useT()
  const servedBy = useClusterStore((s) => s.clusterStatus?.cluster.served_by)

  return (
    <footer className="absolute inset-x-0 bottom-0 py-2 text-center text-[11px] leading-none text-muted-foreground/70">
      {servedBy
        ? t('footer.connected_to', { node: servedBy })
        : t('footer.disconnected')}
    </footer>
  )
}
