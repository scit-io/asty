import { Skeleton } from '@/components/ui/skeleton'

// LoadingBlock — the project-standard pre-data placeholder. Every
// data-driven page renders one while its SSE warms up, and the
// Configuration card uses it before the runtime arrives. Centralised
// so changing the placeholder height is one edit, not seven.
export function LoadingBlock() {
  return <Skeleton className="h-32 w-full" />
}
