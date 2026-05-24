import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Fragment, type ReactNode } from 'react'

export interface Crumb {
  // ReactNode lets pages drop a Skeleton in while the SSE warms up,
  // so the layout slot is reserved and the text doesn't pop in over
  // a fallback (or worse, replace a wrong value on deep-link).
  label: ReactNode
  to?: string // omit on the last item; it renders as the current page
  // key — stable identifier for React. Default: index. Pass it when
  // `label` is a Skeleton (no usable string for the key).
  key?: string
}

// Breadcrumbs is a thin convenience wrapper over shadcn's Breadcrumb
// primitives. Pages pass an array of crumbs; the helper handles
// separators and the "last one is current" rule.
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {items.map((item, i) => (
          <Fragment key={item.key ?? i}>
            <BreadcrumbItem>
              {item.to ? (
                <BreadcrumbLink to={item.to}>{item.label}</BreadcrumbLink>
              ) : (
                <BreadcrumbPage>{item.label}</BreadcrumbPage>
              )}
            </BreadcrumbItem>
            {i < items.length - 1 && <BreadcrumbSeparator />}
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  )
}
