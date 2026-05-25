import { useMemo } from 'react'
import { routes } from '@/lib/routes'
import { useT } from '@/lib/i18n'
import type { TabItem } from '@/components/resource-tabs'

// Top-level Services section tabs. Currently a single entry — the
// global Header collapses sections with one child into a direct link.
// Lives here for symmetry with useClusterTabs so the Header pulls every
// section's tabs from the same convention.
export function useServicesTabs(): TabItem[] {
  const t = useT()
  return useMemo(() => [
    { to: routes.services, label: t('tabs.overview') },
  ], [t])
}
