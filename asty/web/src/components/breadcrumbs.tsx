import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Fragment } from 'react'

export interface Crumb {
  label: string
  to?: string // omit on the last item; it renders as the current page
}

// Breadcrumbs is a thin convenience wrapper over shadcn's Breadcrumb
// primitives. Pages pass an array of crumbs; the helper handles
// separators and the "last one is current" rule.
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {items.map((item, i) => (
          <Fragment key={`${item.label}-${i}`}>
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
