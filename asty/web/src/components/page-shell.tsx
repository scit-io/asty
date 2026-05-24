import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface PageShellProps {
  children: ReactNode
  // tall — logs viewports: h-full + flex column + overflow-hidden so
  // the LogsView card owns the scroll context, while the page itself
  // never overflows.
  tall?: boolean
  // bare — drop the default space-y-4 between children. Used by the
  // skeleton placeholders that render inside early `if (!x) return`
  // branches: those have their own `mb-4` and a uniform vertical
  // rhythm would push the second skeleton too far down.
  bare?: boolean
  className?: string
}

// PageShell is the single page-body wrapper. Three flavours collapse
// into one component:
//   default — container + horizontal padding + vertical space-y-4
//   tall    — same, plus flex column + h-full + overflow-hidden
//   bare    — same, minus space-y-4
// The header lives in its own component (sticky top-bar), so PageShell
// owns only the section under it.
export function PageShell({ children, tall, bare, className }: PageShellProps) {
  const base = tall
    ? 'container mx-auto flex h-full flex-col gap-4 overflow-hidden p-4 sm:p-6'
    : bare
      ? 'container mx-auto p-4 sm:p-6'
      : 'container mx-auto p-4 sm:p-6 space-y-4'
  return <div className={cn(base, className)}>{children}</div>
}
